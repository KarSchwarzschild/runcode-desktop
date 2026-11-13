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
			return localusage.WrapPlatformProvider(base, func(ctx context.Context) (localusage.Scope, error) {
				sf, err := freeze(ctx)
				if err != nil {
					return localusage.Scope{}, err
				}
				if sf.TenantID == "" || sf.AccountSub == "" {
					return localusage.Scope{}, fmt.Errorf("采集身份不足")
				}
				return sf.toScope(), nil
			}, rec), nil
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
	a.usageStore = store
	setPlatformCapture(store, a.freezeUsageScope)
	return nil
}

func (a *App) closeLocalUsage() {
	setPlatformCapture(nil, nil)
	if a.usageStore != nil {
		_ = a.usageStore.Close()
		a.usageStore = nil
	}
}

func (a *App) freezeUsageScope(ctx context.Context) (ScopeFreeze, error) {
	_ = ctx
	a.mu.Lock()
	passport := a.livePassport || a.configPassport
	tenant := a.livePassportTenant
	if tenant == "" {
		tenant = a.passportTenant
	}
	a.mu.Unlock()
	// 身份冻结：只认通行证装配路径，不凭 BaseURL／模型名猜测。
	if !passport || !a.tokens.LoggedIn() {
		return ScopeFreeze{}, fmt.Errorf("非平台连接")
	}
	iss, sub := a.tokens.identityParts()
	if iss == "" || sub == "" {
		return ScopeFreeze{}, fmt.Errorf("登录身份不足")
	}
	return ScopeFreeze{
		AccountIssuer: iss,
		AccountSub:    sub,
		BridgeBase:    passportConfig().BridgeBaseURL,
		TenantID:      tenant,
	}, nil
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
func wrapVisionForCapture(p llm.Provider, platform bool) llm.Provider {
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
	return localusage.WrapPlatformProviderKind(p, func(ctx context.Context) (localusage.Scope, error) {
		sf, err := freeze(ctx)
		if err != nil {
			return localusage.Scope{}, err
		}
		return sf.toScope(), nil
	}, rec, localusage.CallKindVision)
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
