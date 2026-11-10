package desktop

import (
	"errors"
	"strings"
	"sync"

	appwire "github.com/wt68/runcode/internal/protocol"
)

// 会话级「生成图片」开关与每个独立回合的冻结快照。
//
// 开关只表示"允许模型在这个会话里提出生图调用"，不跳过审批；它按会话 + 身份分区，
// 存在会话工作区的 sidecar 里。新会话、分叉会话（新的会话 id）默认关闭。
//
// 快照在回合开始时冻结一次（onTurnStart），整个回合——包括运行中追加的消息与
// 等待中的审批——都用同一份模型、尺寸、TurnID 与一次生成预算。冻结不发网络请求；
// 真正提交前再做权威校验。

const imageGenerationIneligibleReason = "仅平台通行证普通会话支持图片生成"

// imageGenerationBinding is the identity a session's image generation state
// belongs to. Account/Epoch come from the entry's immutable knowledge binding
// (taken when the entry was published), never from whoever is logged in now.
type imageGenerationBinding struct {
	Account, Epoch, Bridge, Tenant, Scope string
}

// imageGenerationSnapshot is frozen per independent turn.
type imageGenerationSnapshot struct {
	Enabled   bool
	Available bool
	Reason    string

	SessionID  string
	TurnID     string
	Generation string
	Workspace  string

	Account, Epoch, Bridge, Tenant, ScopeID string

	SwitchRevision   int64
	SettingsRevision int64
	Model            string
	Size             string
	SupportedSizes   []string
	ResponseFormat   string
}

// imageGenerationTurnState is one session instance's image generation state.
type imageGenerationTurnState struct {
	mu         sync.Mutex
	sessionID  string
	generation string // host session instance generation
	snap       imageGenerationSnapshot
}

func newImageGenerationTurnState(sessionID, generation string) *imageGenerationTurnState {
	return &imageGenerationTurnState{sessionID: sessionID, generation: generation}
}

func (s *imageGenerationTurnState) snapshot() imageGenerationSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snap
	out.SupportedSizes = append([]string(nil), s.snap.SupportedSizes...)
	return out
}

func (s *imageGenerationTurnState) setSnapshot(snap imageGenerationSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap.SupportedSizes = append([]string(nil), snap.SupportedSizes...)
	s.snap = snap
}

// imageGenerationBindingOf returns the session's identity partition, or the
// reason this session cannot use image generation at all.
func (a *App) imageGenerationBindingOf(e *sessionEntry) (imageGenerationBinding, string) {
	a.mu.Lock()
	passport, chat, account, epoch, tenant := e.passport, e.chat != nil, e.knowledge.Account, e.knowledge.Epoch, e.tenantID
	a.mu.Unlock()
	if !passport || chat {
		return imageGenerationBinding{}, imageGenerationIneligibleReason
	}
	switch account {
	case "":
		return imageGenerationBinding{}, errImageGenerationIdentity.Error()
	case "anonymous":
		return imageGenerationBinding{}, errImageGenerationLogin.Error()
	}
	bridge := imageGenerationBridge()
	tenant = strings.TrimSpace(tenant)
	return imageGenerationBinding{Account: account, Epoch: epoch, Bridge: bridge, Tenant: tenant, Scope: imageGenerationScope(account, bridge, tenant)}, ""
}

// imageGenerationAccountCurrent reports whether the logged-in account is
// still the one the session was bound to.
func (a *App) imageGenerationAccountCurrent(b imageGenerationBinding) bool {
	account, epoch, err := a.imageGenerationAccount()
	return err == nil && account == b.Account && epoch == b.Epoch
}

// imageGenerationTarget resolves the scope's saved default for a session, or
// why there is none.
func imageGenerationTarget(b imageGenerationBinding, cfg desktopConfig) (imageGenerationPreference, string) {
	pref := imageGenerationPreferenceFor(cfg, b.Scope)
	if pref.DefaultModel == nil {
		return pref, "尚未选择生图模型，请在「设置 → 图片生成」中选择"
	}
	if *pref.DefaultModel != platformModelRef(b.Tenant, pref.DefaultModel.Name) {
		return pref, "生图模型所属平台或租户已变化，请在设置中重新选择"
	}
	return pref, ""
}

func (a *App) explicitImageGenerationEntry(sessionID string) (*sessionEntry, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errNoSession
	}
	return a.entryOf(sessionID)
}

func (a *App) sessionImageGenerationView(e *sessionEntry, b imageGenerationBinding, sw imageGenerationSwitch, reason string) appwire.SessionImageGenerationView {
	view := appwire.SessionImageGenerationView{SessionID: e.id, Enabled: sw.Enabled, Revision: sw.Revision, Reason: reason}
	if reason != "" {
		return view
	}
	if !a.imageGenerationAccountCurrent(b) {
		view.Reason = "会话账号已变化，请重新打开会话"
		return view
	}
	pref, why := imageGenerationTarget(b, loadRawConfig())
	if why != "" {
		view.Reason = why
		return view
	}
	ref := *pref.DefaultModel
	view.Available, view.Model, view.Size = true, &ref, pref.DefaultSize
	return view
}

// GetSessionImageGeneration reads the session's confirmed switch. A damaged
// record disables image generation without failing ordinary chat.
func (a *App) GetSessionImageGeneration(sessionID string) (appwire.SessionImageGenerationView, error) {
	e, err := a.explicitImageGenerationEntry(sessionID)
	if err != nil {
		return appwire.SessionImageGenerationView{}, wireError(err)
	}
	b, reason := a.imageGenerationBindingOf(e)
	if reason != "" {
		return a.sessionImageGenerationView(e, b, imageGenerationSwitch{}, reason), nil
	}
	disk, err := readImageGenerationSession(e.workspace, e.id)
	if err != nil {
		return a.sessionImageGenerationView(e, b, imageGenerationSwitch{}, "图片生成开关记录无法读取，本会话暂不能生成图片；普通聊天不受影响"), nil
	}
	return a.sessionImageGenerationView(e, b, disk.Scopes[b.Scope], ""), nil
}

// SetSessionImageGeneration changes the session's switch for the next turn.
// It is refused while a turn runs: that turn keeps its frozen snapshot, and a
// changed switch would only take effect afterwards anyway.
func (a *App) SetSessionImageGeneration(req appwire.SetSessionImageGenerationRequest) (appwire.SessionImageGenerationView, error) {
	e, err := a.explicitImageGenerationEntry(req.SessionID)
	if err != nil {
		return appwire.SessionImageGenerationView{}, wireError(err)
	}
	b, reason := a.imageGenerationBindingOf(e)
	if reason != "" {
		return appwire.SessionImageGenerationView{}, wireError(errors.New(reason))
	}
	if !a.imageGenerationAccountCurrent(b) {
		return appwire.SessionImageGenerationView{}, wireError(errors.New("会话账号已变化，请重新打开会话"))
	}
	// questionMu keeps a submission from being prepared while the switch is
	// being replaced; turnActive covers a turn already running.
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	a.mu.Lock()
	busy, current := e.turnActive, a.entryLocked(e.id) == e && !e.closed
	a.mu.Unlock()
	if !current {
		return appwire.SessionImageGenerationView{}, wireError(errors.New("会话已变化，请重新打开会话"))
	}
	if busy {
		return appwire.SessionImageGenerationView{}, wireError(errors.New("回合进行中不能切换图片生成，请等待完成或停止后再试"))
	}
	disk, err := readImageGenerationSession(e.workspace, e.id)
	if err != nil {
		return appwire.SessionImageGenerationView{}, wireError(err)
	}
	sw := disk.Scopes[b.Scope]
	if sw.Enabled == req.Enabled {
		return a.sessionImageGenerationView(e, b, sw, ""), nil
	}
	if sw.Revision != req.ExpectedRevision {
		return appwire.SessionImageGenerationView{}, wireError(errors.New("图片生成开关已变化，请刷新后重试"))
	}
	next := imageGenerationSwitch{Enabled: req.Enabled, Revision: sw.Revision + 1}
	disk.Scopes[b.Scope] = next
	if err := writeImageGenerationSession(e.workspace, e.id, disk); err != nil {
		return appwire.SessionImageGenerationView{}, wireError(err)
	}
	return a.sessionImageGenerationView(e, b, next, ""), nil
}

// freezeImageGenerationTurn reserves a fresh TurnID and freezes the target
// for one independent turn. Only the turn-start hook calls it; injected
// messages and resumed turns keep the snapshot they already have.
func (a *App) freezeImageGenerationTurn(e *sessionEntry) {
	if e == nil {
		return
	}
	a.mu.Lock()
	state, workspace := e.imageGeneration, e.workspace
	a.mu.Unlock()
	if state == nil {
		return
	}
	snap := imageGenerationSnapshot{
		SessionID:  e.id,
		TurnID:     newImageGenerationID("turn_"),
		Generation: state.generation,
		Workspace:  workspace,
	}
	b, reason := a.imageGenerationBindingOf(e)
	if reason == "" {
		snap.Account, snap.Epoch, snap.Bridge, snap.Tenant, snap.ScopeID = b.Account, b.Epoch, b.Bridge, b.Tenant, b.Scope
		disk, err := readImageGenerationSession(workspace, e.id)
		if err != nil {
			reason = "图片生成开关记录无法读取，本回合不能生成图片"
		} else {
			sw := disk.Scopes[b.Scope]
			snap.Enabled, snap.SwitchRevision = sw.Enabled, sw.Revision
			if !sw.Enabled {
				reason = "当前会话未开启图片生成"
			}
		}
	}
	if reason == "" {
		pref, why := imageGenerationTarget(b, loadRawConfig())
		snap.SettingsRevision = pref.Revision
		if why != "" {
			reason = why
		} else {
			snap.Model, snap.Size, snap.SupportedSizes, snap.ResponseFormat = pref.DefaultModel.Name, pref.DefaultSize, pref.SupportedSizes, pref.ResponseFormat
			if len(snap.SupportedSizes) == 0 {
				snap.SupportedSizes = []string{pref.DefaultSize}
			}
		}
	}
	snap.Reason, snap.Available = reason, reason == ""
	state.setSnapshot(snap)
}
