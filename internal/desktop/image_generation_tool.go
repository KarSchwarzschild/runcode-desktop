package desktop

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/wt68/runcode/internal/imagegentool"
	appwire "github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

// image_generate 的宿主一侧：注册到合格的通行证主会话、把冻结快照交给工具、在真正
// 提交前做权威复核、持久化提交账本、发出唯一一次请求并落盘结果。
//
// 复核点有三个——请求前（目录与权限）、提交前（账本已落盘后再看一次身份与会话实例）、
// 发布前（落盘后、交回结果前）。任一处会话已关闭/重建、账号已变化，都不再前进；
// 前两处 Core POST 为 0，第三处结果不投递（图片可能已生成，如实说明）。

const imageGenerationOALocked = "本会话读取过 OA 数据，不能把对话内容发送给生图模型；本次未生成图片"

// imageGenerationTools registers image_generate for a Passport main session.
// ExtraTools never reach sub-agents, so they structurally cannot generate.
func (a *App) imageGenerationTools(cfg *engine.Config, sessionID, generation string) []tool.Tool {
	if !usingPassport(cfg) {
		return nil
	}
	state := newImageGenerationTurnState(sessionID, generation)
	a.mu.Lock()
	a.pendingImageGeneration = state
	a.mu.Unlock()
	return []tool.Tool{a.newImageGenerationTool(state)}
}

func (a *App) newImageGenerationTool(state *imageGenerationTurnState) *imagegentool.Tool {
	return &imagegentool.Tool{
		Host:         imageGenerationHost{app: a, state: state},
		SnapshotFunc: func() imagegentool.Snapshot { return a.imageGenerationToolSnapshot(state) },
	}
}

func imageGenerationEndpoint(snap imageGenerationSnapshot) string {
	if snap.Bridge == "" {
		return ""
	}
	return snap.Bridge + tenantPathPrefix(snap.Tenant) + "/v1/Images/Generations"
}

// imageGenerationToolSnapshot is the tool's view of the frozen turn.
// BindingIdentity pins approvals to everything that decides where the paid
// request goes; the tool adds the effective size.
func (a *App) imageGenerationToolSnapshot(state *imageGenerationTurnState) imagegentool.Snapshot {
	s := state.snapshot()
	out := imagegentool.Snapshot{
		Enabled: s.Enabled, Available: s.Available, Reason: s.Reason,
		SessionID: s.SessionID, TurnID: s.TurnID, Model: s.Model, Size: s.Size, SupportedSizes: s.SupportedSizes,
		BindingIdentity: tool.BindingFingerprint(imageGenerationScopeVersion, s.ScopeID, s.Bridge, s.Tenant, s.Model, s.TurnID,
			itoa64(s.SwitchRevision), itoa64(s.SettingsRevision), s.Generation),
		Endpoint: imageGenerationEndpoint(s),
	}
	if out.Available && a.oaLockedModel(s.SessionID) != "" {
		out.Available, out.Reason = false, imageGenerationOALocked
	}
	return out
}

type imageGenerationHost struct {
	app   *App
	state *imageGenerationTurnState
}

func imageGenerationRejected(snap imageGenerationSnapshot, size, code, message string) appwire.ImageGenerationResult {
	return appwire.ImageGenerationResult{Version: 1, Status: "failed", Code: code, Model: snap.Model, Size: size, Images: []appwire.ImageGenerationArtifact{}, Message: message, AccountingStatus: "unverified"}
}

// checkImageGenerationLive confirms the frozen turn still belongs to the live
// session instance and account. It holds App.mu only for the entry check.
func (a *App) checkImageGenerationLive(snap imageGenerationSnapshot, state *imageGenerationTurnState) (code string, err error) {
	a.mu.Lock()
	e := a.entryLocked(snap.SessionID)
	current := e != nil && !e.closed && e.imageGeneration == state
	a.mu.Unlock()
	if !current {
		return "", errors.New("会话已关闭或已重建，本次未生成图片")
	}
	account, epoch, err := a.imageGenerationAccount()
	if err != nil || account != snap.Account || epoch != snap.Epoch {
		return "auth", errors.New("会话账号已变化或已退出登录，本次未生成图片；请重新打开会话")
	}
	if snap.Bridge != imageGenerationBridge() {
		return "model_unavailable", errors.New("生图服务地址已变化，本次未生成图片；请重新打开会话")
	}
	return "", nil
}

// Generate implements imagegentool.Host.
func (h imageGenerationHost) Generate(ctx context.Context, in imagegentool.Input, meta imagegentool.CallMeta) (appwire.ImageGenerationResult, error) {
	a, snap := h.app, h.state.snapshot()
	if snap.TurnID != meta.TurnID || snap.SessionID != meta.SessionID || !snap.Available {
		return imageGenerationRejected(snap, in.Size, "invalid_input", "回合已变化，本次未生成图片"), nil
	}
	if code, err := a.checkImageGenerationLive(snap, h.state); err != nil {
		return imageGenerationRejected(snap, in.Size, code, err.Error()), nil
	}
	if a.oaLockedModel(snap.SessionID) != "" {
		return imageGenerationRejected(snap, in.Size, "forbidden", imageGenerationOALocked), nil
	}

	// Authoritative check against the live catalog: a model withdrawn or a size
	// dropped after approval is refused here, before anything is sent.
	checkCtx, cancel := context.WithTimeout(ctx, imageGenerationCatalogTimeout)
	catalog, err := a.freshImageGenerationCatalog(checkCtx, snap.Tenant, a.visionAccountIdentity())
	cancel()
	if err != nil {
		return imageGenerationRejected(snap, in.Size, "model_unavailable", "无法确认生图模型当前可用，本次未生成图片"), nil
	}
	entry, ok := catalog.imageGeneration[snap.Model]
	profile := normalizeImageGenerationProfile(entry.ImageGeneration)
	if !ok || !profile.Available || !slices.Contains(profile.SupportedSizes, in.Size) {
		return imageGenerationRejected(snap, in.Size, "model_unavailable", "该生图模型或尺寸当前不可用，本次未生成图片；请在设置中重新选择"), nil
	}
	if snap.ResponseFormat = preferredResponseFormat(profile.ResponseFormats); snap.ResponseFormat == "" {
		return imageGenerationRejected(snap, in.Size, "model_unavailable", "该生图模型的返回格式不受支持，本次未生成图片"), nil
	}

	generationID := newImageGenerationID("gen_")
	if err := reserveImageGenerationTurn(snap.Workspace, snap.SessionID, snap.TurnID, generationID); err != nil {
		if errors.Is(err, errImageGenerationTurnSpent) {
			return imageGenerationRejected(snap, in.Size, "turn_budget_exhausted", "本回合已经提交过一次图片生成，不能再次生成"), nil
		}
		return imageGenerationRejected(snap, in.Size, "", "无法记录本次提交，本次未生成图片"), nil
	}
	rec := imageGenerationLedger{
		GenerationID: generationID, SessionID: snap.SessionID, TurnID: snap.TurnID, QuestionID: meta.MessageID, ToolUseID: meta.ToolUseID,
		ScopeID: snap.ScopeID, Model: snap.Model, Size: in.Size, Status: "submitted", AccountingStatus: "unverified", SubmittedAt: time.Now().UTC(),
	}
	if err := writeImageGenerationLedger(snap.Workspace, rec); err != nil {
		return imageGenerationRejected(snap, in.Size, "", "无法记录本次提交，本次未生成图片"), nil
	}
	finish := func(result appwire.ImageGenerationResult) (appwire.ImageGenerationResult, error) {
		result.Version, result.GenerationID, result.Model, result.Size = 1, generationID, snap.Model, in.Size
		rec = rec.finalized(result)
		if err := writeImageGenerationLedger(snap.Workspace, rec); err != nil {
			debugLog("image generation ledger %s not finalized: %v", generationID, err)
		}
		return rec.effectiveResult(), nil
	}
	if code, err := a.checkImageGenerationLive(snap, h.state); err != nil {
		return finish(appwire.ImageGenerationResult{Status: "failed", Code: code, Message: err.Error()})
	}

	resp, err := a.postImageGeneration(ctx, snap, in)
	if err != nil {
		f := imageGenerationFailureOf(err)
		return finish(appwire.ImageGenerationResult{Status: f.Status, Code: f.Code, Message: f.Message, RequestID: f.RequestID})
	}
	art, err := saveGeneratedImage(ctx, snap.Workspace, generationID, resp.Data[0])
	if err != nil {
		return finish(appwire.ImageGenerationResult{Status: "save_failed", Code: "save_failed", RequestID: resp.requestID,
			Message: "已收到生成结果，但下载或保存失败，计费待核验；不会自动重新生成"})
	}
	if _, err := a.checkImageGenerationLive(snap, h.state); err != nil {
		return finish(appwire.ImageGenerationResult{Status: "cancelled", RequestID: resp.requestID,
			Message: "会话或账号已变化，结果未投递到当前会话；图片可能已生成并计费"})
	}
	return finish(appwire.ImageGenerationResult{Status: "succeeded", RequestID: resp.requestID, Images: []appwire.ImageGenerationArtifact{art}})
}
