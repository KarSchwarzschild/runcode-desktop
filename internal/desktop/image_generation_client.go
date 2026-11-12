package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/wt68/runcode/internal/imagegentool"
)

// 生图请求：发往业务网关 POST {Bridge}/t/{tenant}/v1/Images/Generations（设计 6.2）。
//
// 任何已经发出的 POST 都不重试——401/403/429/5xx、超时、断连、空/多图都不重发，
// 也不走目录那套「401 刷新令牌后重发」。发出前可以正常取得/刷新令牌。错误只按网关
// 自己声明的错误码分类；Core 的原文、HTML、堆栈一律不回显，也不从文字里猜额度/限流/
// 内容拒绝。

var imageGenerationPostTimeout = 180 * time.Second

const imageGenerationResponseLimit = 32 << 20

const (
	imageGenerationMsgUnknown   = "生成结果无法确认，可能已生成或计费，请勿重复提交；如需核查请联系管理员"
	imageGenerationMsgGeneric   = "生成服务未返回可确认的结果，可能已生成或计费，请联系管理员核查"
	imageGenerationMsgCancelled = "已停止等待；请求已发出，上游可能已生成或计费"
)

// bridgeImageErrors are the Bridge's own, provable rejections (design 6.3): the
// request never reached Core. Each is accepted only with its declared status.
var bridgeImageErrors = map[string]struct {
	status  int
	message string
}{
	"auth":              {http.StatusUnauthorized, "通行证登录已失效，本次未生成图片；请重新登录后再提出生成要求"},
	"forbidden":         {http.StatusForbidden, "当前账号或租户无权使用该生图模型，本次未生成图片"},
	"invalid_input":     {http.StatusBadRequest, "生成参数不被接受（如尺寸或提示词），本次未生成图片"},
	"model_unavailable": {http.StatusServiceUnavailable, "该生图模型当前不可用，本次未生成图片；请在设置中重新选择"},
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type imageGenerationWireRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
}

type imageGenerationWireImage struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// imageGenerationWireResponse mirrors Core's created/data[]. requestID comes
// only from the Bridge's X-Request-ID header, never from the body.
type imageGenerationWireResponse struct {
	Created   int64                      `json:"created"`
	Data      []imageGenerationWireImage `json:"data"`
	requestID string
}

// imageGenerationFailure is a classified non-success outcome. Status is failed
// only when the request provably produced nothing.
type imageGenerationFailure struct {
	Status    string
	Code      string
	Message   string
	RequestID string
}

func (f *imageGenerationFailure) Error() string { return f.Message }

// imageGenerationFailureOf classifies any error; an unclassified one is unknown.
func imageGenerationFailureOf(err error) imageGenerationFailure {
	if err == nil {
		return imageGenerationFailure{}
	}
	var f *imageGenerationFailure
	if errors.As(err, &f) {
		return *f
	}
	return imageGenerationFailure{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgUnknown}
}

func notSent(code, message string) error {
	return &imageGenerationFailure{Status: "failed", Code: code, Message: message}
}

func bridgeRequestID(h http.Header) string {
	id := strings.TrimSpace(h.Get("X-Request-ID"))
	if requestIDPattern.MatchString(id) {
		return id
	}
	return ""
}

func imageGenerationHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A fresh connection per generation: no reused-connection replay edge cases.
	transport.DisableKeepAlives = true
	return &http.Client{
		Transport: transport,
		// Never follow a redirect: it could carry the Bearer elsewhere, and a POST
		// that was redirected is not a result we can confirm.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// postImageGeneration sends exactly one generation request for the frozen
// snapshot. The caller must have durably recorded the submission first.
func (a *App) postImageGeneration(ctx context.Context, snap imageGenerationSnapshot, in imagegentool.Input) (imageGenerationWireResponse, error) {
	if snap.Account == "" || snap.Bridge == "" || snap.Model == "" || in.Size == "" || (snap.ResponseFormat != "url" && snap.ResponseFormat != "b64_json") {
		return imageGenerationWireResponse{}, notSent("not_configured", "生图配置不完整，本次未生成图片")
	}
	if snap.Bridge != imageGenerationBridge() {
		return imageGenerationWireResponse{}, notSent("model_unavailable", "生图服务地址已变化，本次未生成图片；请重新打开会话")
	}
	token, err := a.tokens.Token()
	if err != nil {
		return imageGenerationWireResponse{}, notSent("auth", "请先登录通行证；本次未生成图片")
	}
	if tokenIdentity(token) != snap.Account || a.tokens.loginEpoch() != snap.Epoch {
		return imageGenerationWireResponse{}, notSent("auth", "会话账号已变化，本次未生成图片；请重新打开会话")
	}
	body, _ := json.Marshal(imageGenerationWireRequest{Model: snap.Model, Prompt: in.Prompt, N: 1, Size: in.Size, ResponseFormat: snap.ResponseFormat})
	postCtx, cancel := context.WithTimeout(ctx, imageGenerationPostTimeout)
	defer cancel()
	endpoint := snap.Bridge + tenantPathPrefix(snap.Tenant) + "/v1/Images/Generations"
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return imageGenerationWireResponse{}, notSent("not_configured", "生图服务地址无效，本次未生成图片")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := imageGenerationHTTPClient().Do(req)
	if err != nil {
		return imageGenerationWireResponse{}, interruptedOutcome(ctx, "")
	}
	defer func() { _ = resp.Body.Close() }()
	requestID := bridgeRequestID(resp.Header)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, imageGenerationResponseLimit+1))
	if err != nil {
		return imageGenerationWireResponse{}, interruptedOutcome(ctx, requestID)
	}
	if len(raw) > imageGenerationResponseLimit {
		return imageGenerationWireResponse{}, &imageGenerationFailure{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgUnknown, RequestID: requestID}
	}
	if resp.StatusCode != http.StatusOK {
		return imageGenerationWireResponse{}, classifyImageGenerationStatus(resp.StatusCode, raw, requestID)
	}
	var out imageGenerationWireResponse
	if json.Unmarshal(raw, &out) != nil || len(out.Data) != 1 || (out.Data[0].URL == "" && out.Data[0].B64JSON == "") {
		// 2xx without exactly one usable result may still have been generated and charged.
		return imageGenerationWireResponse{}, &imageGenerationFailure{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgUnknown, RequestID: requestID}
	}
	out.requestID = requestID
	return out, nil
}

// interruptedOutcome is the result of a request that was sent (or may have
// been) but produced no complete response.
func interruptedOutcome(ctx context.Context, requestID string) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &imageGenerationFailure{Status: "cancelled", Message: imageGenerationMsgCancelled, RequestID: requestID}
	}
	return &imageGenerationFailure{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgUnknown, RequestID: requestID}
}

func classifyImageGenerationStatus(status int, raw []byte, requestID string) error {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	code := ""
	if json.Unmarshal(raw, &envelope) == nil {
		code = envelope.Error.Code
	}
	if known, ok := bridgeImageErrors[code]; ok && known.status == status {
		return &imageGenerationFailure{Status: "failed", Code: code, Message: known.message, RequestID: requestID}
	}
	if code == "unknown_outcome" {
		return &imageGenerationFailure{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgUnknown, RequestID: requestID}
	}
	// Anything else — Core's opaque 500s, internal service 401/403 wrapped by the
	// Bridge, gateways' HTML pages — is not evidence that nothing was generated.
	return &imageGenerationFailure{Status: "unknown", Code: "generation_failed", Message: imageGenerationMsgGeneric, RequestID: requestID}
}
