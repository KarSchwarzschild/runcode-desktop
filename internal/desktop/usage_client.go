package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/wt68/runcode/internal/protocol"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// 用量查询的 Bridge 读取层：只发 GET、只访问已配置 Bridge 的 /api/usage/*，带当前
// 通行证令牌；身份与租户授权由 Bridge 判定，这里不传任何 userId / scope。

const (
	usageMaxResponseBytes = 4 << 20
	// usageIdentityPoll 是请求期间复查登录代际的间隔：登出或换号后旧请求要尽快放手，
	// 不能等到 20 秒预算用完。
	usageIdentityPoll = 50 * time.Millisecond
)

var usageRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func usageError(code, message, requestID string) error {
	pe := &hostproto.Error{Code: code, Message: message}
	if requestID != "" && usageRequestIDPattern.MatchString(requestID) {
		pe.Details = map[string]string{"requestId": requestID}
	}
	return pe
}

func usageInvalidArgument(message string) error {
	return usageError(hostproto.ErrCodeInvalidArgument, message, "")
}

func usageInvalidResponse() error {
	return usageError(protocol.ErrCodeUsageInvalidResponse, "用量服务返回的数据不完整或不一致，已丢弃，请稍后重试", "")
}

func usageNotLoggedIn() error {
	return usageError(hostproto.ErrCodeNotLoggedIn, "请先登录通行证后查看用量", "")
}

func usageIdentityChanged() error {
	return usageError(protocol.ErrCodeUsageIdentityChanged, "查询期间登录账号已变化，旧结果已作废", "")
}

func usageUnavailable(requestID string) error {
	return usageError(protocol.ErrCodeUsageUnavailable, "用量服务暂不可用，请稍后重试", requestID)
}

func usageTimeout(requestID string) error {
	return usageError(protocol.ErrCodeUsageTimeout, "统计未能在时限内完成，可缩小时间范围后重试", requestID)
}

// usageStatusError 把 Bridge 的失败状态翻成稳定错误码。上游 message 不透传：它可能
// 带内部细节，界面只显示固定文案与可报障的 requestId。
func usageStatusError(status int, body []byte) error {
	var payload struct {
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(body, &payload)
	rid := payload.RequestID
	switch status {
	case http.StatusUnauthorized:
		return usageError(hostproto.ErrCodeNotLoggedIn, "通行证登录已失效，请重新登录后查看用量", rid)
	case http.StatusForbidden:
		return usageError(protocol.ErrCodeUsageForbidden, "当前账号无权查看所选租户的用量", rid)
	case http.StatusBadRequest:
		if payload.Code == "invalid_cursor" {
			return usageError(protocol.ErrCodeUsageInvalidCursor, "分页已失效，请回到第一页重新查看", rid)
		}
		return usageError(protocol.ErrCodeUsageInvalidRange, "时间范围或筛选条件无效，请调整后重试", rid)
	case http.StatusUnprocessableEntity:
		return usageError(protocol.ErrCodeUsageUnsupported, "当前数据暂不支持这项统计，未显示的指标不代表零用量", rid)
	case http.StatusGatewayTimeout:
		return usageTimeout(rid)
	default:
		return usageUnavailable(rid)
	}
}

// usageGet 读取一个用量端点并解码到 target。整个过程绑定发起时的登录代际：期间
// 登出或换号就放弃请求并返回 identity_changed，旧账号的结果绝不交给调用方。
// 401 至多强制刷新令牌并重试一次；其他失败不重试（读取虽然安全，但重试只会把
// 一次已经超时或被限流的统计再压到数据库上）。
func (a *App) usageGet(ctx context.Context, path string, query url.Values, target any) error {
	if a.tokens == nil || !a.tokens.LoggedIn() {
		return usageNotLoggedIn()
	}
	epoch := a.tokens.loginEpoch()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.cancelOnIdentityChange(ctx, cancel, epoch)

	base, err := url.Parse(passportConfig().BridgeBaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return usageUnavailable("")
	}
	endpoint := passportConfig().BridgeBaseURL + path + "?" + query.Encode()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for attempt := 0; attempt < 2; attempt++ {
		token, err := a.usageToken(ctx, attempt == 1)
		if err != nil {
			return a.usageContextError(ctx, epoch, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return usageUnavailable("")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return a.usageContextError(ctx, epoch, usageUnavailable(""))
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, usageMaxResponseBytes+1))
		_ = resp.Body.Close()
		if a.tokens.loginEpoch() != epoch {
			return usageIdentityChanged()
		}
		if readErr != nil {
			return a.usageContextError(ctx, epoch, usageUnavailable(""))
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return usageStatusError(resp.StatusCode, body)
		}
		if len(body) > usageMaxResponseBytes || json.Unmarshal(body, target) != nil {
			return usageInvalidResponse()
		}
		return nil
	}
	return usageNotLoggedIn()
}

func (a *App) cancelOnIdentityChange(ctx context.Context, cancel context.CancelFunc, epoch string) {
	tick := time.NewTicker(usageIdentityPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if a.tokens.loginEpoch() != epoch {
				cancel()
				return
			}
		}
	}
}

// usageContextError 区分「账号变了」「时限到了」「调用方放弃了」与普通失败。
func (a *App) usageContextError(ctx context.Context, epoch string, fallback error) error {
	switch {
	case a.tokens.loginEpoch() != epoch:
		return usageIdentityChanged()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return usageTimeout("")
	case ctx.Err() != nil:
		return usageError(protocol.ErrCodeUsageUnavailable, "查询已取消", "")
	}
	return fallback
}

// usageToken 取令牌（refresh 时先强制续期一次）。续期可能卡在通行证上，所以放进
// goroutine，由整条命令的时限兜底。
func (a *App) usageToken(ctx context.Context, refresh bool) (string, error) {
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		if refresh {
			a.tokens.ForceRefresh()
		}
		token, err := a.tokens.Token()
		done <- result{token: token, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", usageUnavailable("")
	case r := <-done:
		if r.err != nil {
			return "", usageNotLoggedIn()
		}
		return r.token, nil
	}
}
