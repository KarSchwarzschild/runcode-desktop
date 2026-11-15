package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wt68/runcode/internal/localusage"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

// 平台调用采集：只统计通行证／平台连接；自定义 Provider 保持原样。
// 调用计数定义：一次底层平台 Stream／生图 POST／搜索逻辑请求计一次；SDK 内不可见重试不拆分。
// RUNCODE_LOCAL_USAGE_ENABLED=false 停止后续采集，历史仍可查（S10 回退开关）。

const envLocalUsageEnabled = "RUNCODE_LOCAL_USAGE_ENABLED"

var (
	platformCaptureOnce sync.Once
	platformCaptureMu   sync.Mutex
	platformRecorder    localusage.Recorder
	platformFreeze      func(context.Context) (ScopeFreeze, error)
	platformEnabled     atomic.Bool
)

// ScopeFreeze 是采集冻结身份（与 localusage.Scope 对应，由宿主构造）。
type ScopeFreeze struct {
	AccountIssuer string
	AccountSub    string
	BridgeBase    string
	TenantID      string
}

func (s ScopeFreeze) toScope() localusage.Scope {
	acc := digestPair(s.AccountIssuer, s.AccountSub)
	br := digestBridge(s.BridgeBase)
	return localusage.Scope{
		ScopeID:       digestPair(acc, br+":"+s.TenantID),
		AccountDigest: acc,
		BridgeDigest:  br,
		TenantID:      s.TenantID,
	}
}

func digestPair(a, b string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(a) + "\x00" + strings.TrimSpace(b)))
	return hex.EncodeToString(sum[:16])
}

func digestBridge(raw string) string {
	u := strings.TrimSpace(strings.TrimRight(raw, "/"))
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		u = strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/")
	}
	sum := sha256.Sum256([]byte(u))
	return hex.EncodeToString(sum[:16])
}

// ensurePlatformCaptureRegistered 只注册一次固定工厂，避免多次 New(App) panic。
func ensurePlatformCaptureRegistered() {
	platformCaptureOnce.Do(func() {
		platformEnabled.Store(localUsageEnvEnabled())
		llm.Register(localusage.PlatformProviderName, func(cfg llm.Config) (llm.Provider, error) {
			base, err := llm.Build("openai", cfg)
			if err != nil {
				return nil, err
			}
			platformCaptureMu.Lock()
			rec := platformRecorder
			freeze := platformFreeze
			platformCaptureMu.Unlock()
			if !platformEnabled.Load() || rec == nil || freeze == nil {
				return base, nil
			}
			return localusage.WrapPlatformProvider(base, platformUsageFreezer(cfg.BaseURL, freeze), rec), nil
		})
	})
}

func localUsageEnvEnabled() bool {
	v := strings.TrimSpace(os.Getenv(envLocalUsageEnabled))
	if v == "" {
		return true
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

// setPlatformCapture 注入记录器和身份冻结函数；测试可以替换这两个依赖。
func setPlatformCapture(rec localusage.Recorder, freeze func(context.Context) (ScopeFreeze, error)) {
	ensurePlatformCaptureRegistered()
	platformCaptureMu.Lock()
	defer platformCaptureMu.Unlock()
	platformRecorder = rec
	platformFreeze = freeze
	platformEnabled.Store(localUsageEnvEnabled() && rec != nil && freeze != nil)
}

func (a *App) initLocalUsage() error {
	ensurePlatformCaptureRegistered()
	if !localUsageEnvEnabled() {
		setPlatformCapture(nil, nil)
		platformEnabled.Store(false)
		return nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "runcode", "usage", "usage.sqlite3")
	store, err := localusage.Open(path, nil)
	if err != nil {
		debugLog("local usage open failed: %v", err)
		return err
	}
	a.usageMu.Lock()
	a.usageStore = store
	a.usageMu.Unlock()
	setPlatformCapture(store, a.freezeUsageIdentity)
	return nil
}

func (a *App) closeLocalUsage() {
	setPlatformCapture(nil, nil)
	// Hold the read/write lock through Close so a query that has acquired a
	// stable pointer can finish before the database is closed. New queries see
	// the nil marker only after the close has completed.
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	store := a.usageStore
	a.usageStore = nil
	if store != nil {
		_ = store.Close()
	}
}

// freezeUsageIdentity 只读取稳定账号；平台连接和租户归属由调用方配置证明，
// 不读取当前前台会话，否则后台会话和平台识图会跟随焦点串租。
func (a *App) freezeUsageIdentity(ctx context.Context) (ScopeFreeze, error) {
	_ = ctx
	iss, sub := a.tokens.identityParts()
	if strings.TrimSpace(iss) == "" || strings.TrimSpace(sub) == "" {
		return ScopeFreeze{}, fmt.Errorf("登录身份不足")
	}
	return ScopeFreeze{
		AccountIssuer: iss,
		AccountSub:    sub,
	}, nil
}

// platformUsageBaseURLScope 仅接受平台 API 的明确 /t/<tenant>/v1 形状。
// Bridge 与租户来自同一份不可变配置；缺失或歧义时不回落到全局设置。
func platformUsageBaseURLScope(raw string) (bridge, tenant string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) < 4 || parts[len(parts)-3] != "t" || parts[len(parts)-1] != "v1" {
		return "", "", false
	}
	tenant = parts[len(parts)-2]
	if !validLocalTenant(tenant) || tenant == "." || tenant == ".." {
		return "", "", false
	}
	u.Path = strings.Join(parts[:len(parts)-3], "/")
	return strings.TrimRight(u.String(), "/"), tenant, true
}

func platformUsageFreezer(baseURL string, identity func(context.Context) (ScopeFreeze, error)) func(context.Context) (localusage.Scope, error) {
	bridge, tenant, ok := platformUsageBaseURLScope(baseURL)
	var diagnostic sync.Once
	gap := func(code string) (localusage.Scope, error) {
		// 只记录安全短码，且每个 provider 最多一次；不泄露 token、URL 或账号。
		diagnostic.Do(func() { debugLog("local usage coverage gap: %s", code) })
		return localusage.Scope{}, fmt.Errorf("采集身份不足: %s", code)
	}
	return func(ctx context.Context) (localusage.Scope, error) {
		if !ok {
			return gap("invalid_platform_scope")
		}
		sf, err := identity(ctx)
		if err != nil || strings.TrimSpace(sf.AccountIssuer) == "" || strings.TrimSpace(sf.AccountSub) == "" {
			return gap("missing_stable_identity")
		}
		sf.BridgeBase, sf.TenantID = bridge, tenant
		return sf.toScope(), nil
	}
}

// freezeUsageScopeParts 用调用方已冻结的 Bridge／租户构造 scope（生图／搜索）。
func (a *App) freezeUsageScopeParts(bridge, tenant string) (localusage.Scope, error) {
	iss, sub := a.tokens.identityParts()
	if iss == "" || sub == "" {
		return localusage.Scope{}, fmt.Errorf("登录身份不足")
	}
	if strings.TrimSpace(bridge) == "" {
		bridge = passportConfig().BridgeBaseURL
	}
	return ScopeFreeze{
		AccountIssuer: iss,
		AccountSub:    sub,
		BridgeBase:    bridge,
		TenantID:      tenant,
	}.toScope(), nil
}

func currentPlatformRecorder() localusage.Recorder {
	platformCaptureMu.Lock()
	defer platformCaptureMu.Unlock()
	if !platformEnabled.Load() {
		return nil
	}
	return platformRecorder
}

// usePlatformCaptureProvider 在已确认平台连接时改用采集适配器名。
func usePlatformCaptureProvider(cfg engine.Config, passport bool) engine.Config {
	if !passport || !platformEnabled.Load() {
		return cfg
	}
	ensurePlatformCaptureRegistered()
	if cfg.Provider == "openai" || cfg.Provider == "" {
		cfg.Provider = localusage.PlatformProviderName
	}
	return cfg
}

// wrapVisionForCapture 包装识图 provider（平台路径）；自定义识图不包装。
func wrapVisionForCapture(p llm.Provider, platform bool, baseURL string) llm.Provider {
	if !platform || !platformEnabled.Load() || p == nil {
		return p
	}
	platformCaptureMu.Lock()
	rec := platformRecorder
	freeze := platformFreeze
	platformCaptureMu.Unlock()
	if rec == nil || freeze == nil {
		return p
	}
	return localusage.WrapPlatformProviderKind(p, platformUsageFreezer(baseURL, freeze), rec, localusage.CallKindVision)
}

// beginNonLLMCapture 在平台生图／搜索真正发出前开始记账；失败则跳过，不阻断业务。
func (a *App) beginNonLLMCapture(ctx context.Context, start localusage.StartRecord) (finish func(status, errCode string, images *int64), began bool) {
	rec := currentPlatformRecorder()
	noop := func(string, string, *int64) {}
	if rec == nil || start.CallID == "" || start.Scope.ScopeID == "" {
		return noop, false
	}
	mono := time.Now()
	if err := rec.Begin(ctx, start); err != nil {
		return noop, false
	}
	return func(status, errCode string, images *int64) {
		dur := time.Since(mono).Milliseconds()
		fin := localusage.FinishRecord{
			CallID: start.CallID, Status: status, FinishedAt: time.Now().UTC(),
			DurationMs: &dur, ErrorCode: errCode, TokenSource: "unavailable",
			GeneratedImages: images, FrozenStart: start,
		}
		_ = rec.Finish(context.Background(), fin)
	}, true
}

// identityParts 从 access token JWT 取稳定 iss／sub；opaque token 无法解析时返回空。
func (m *tokenManager) identityParts() (issuer, sub string) {
	if m == nil {
		return "", ""
	}
	m.mu.Lock()
	token := m.ts.AccessToken
	m.mu.Unlock()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var claims struct {
		Sub string `json:"sub"`
		Iss string `json:"iss"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Sub == "" {
		return "", ""
	}
	return claims.Iss, claims.Sub
}

// isInternalUsageProvider 隐藏内部适配器名，不进入自定义模型可选列表。
func isInternalUsageProvider(name string) bool {
	return name == localusage.PlatformProviderName
}
