package desktop

// 一个回合的生命周期:提交、打断、授权应答、以及回合之外的手动压缩。
// 真正的执行在 host 管理的 goroutine 里,这里只维护桌面侧的记账(在途标记、
// 自动标题的取材、每回合的编辑基线)。

import (
	"context"
	"errors"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
	"gitlab.ouc-online.com.cn/aibase/agentloop/sessions"
)

// SendMessage runs one user turn asynchronously. It returns immediately; the
// turn's result arrives as an EventTurnEnd or EventTurnError. It errors only when
// there is no session or a turn is already running.
func (a *App) SendMessage(sessionID, text string) error {
	return wireError(a.sendUserTurn(sessionID, text, nil, false))
}

// InjectMessage delivers text into the in-flight turn as mid-turn steering: the
// engine splices it into the running turn so the model sees it at the next
// iteration boundary (after the current tool round), instead of only next turn. If
// no turn is running (it just ended in the race window), it falls back to starting
// a fresh turn and returns startedTurn=true so the frontend can flip its busy
// state; a successful mid-turn injection returns false (the running turn's
// lifecycle already drives busy).
func (a *App) InjectMessage(sessionID, text string) (bool, error) {
	return a.injectOrSend(sessionID, text, nil, false)
}

// injectOrSend tries to inject into the live turn and, on ErrNoActiveTurn, sends
// the message as a fresh turn instead. Shared by InjectMessage and
// InjectMessageWithImages.
func (a *App) injectOrSend(sessionID, text string, images []llm.ImageSource, withImages bool) (bool, error) {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	e, err := a.entryOf(sessionID)
	if err != nil {
		return false, wireError(err)
	}
	if !e.questionMu.TryLock() {
		return false, wireError(host.ErrBusy)
	}
	defer e.questionMu.Unlock()
	if err := a.checkKnowledgeSubmission(e, nil); err != nil {
		return false, wireError(err)
	}
	err = a.mgr.Inject(e.id, text, images)
	if err == nil {
		return false, nil // spliced into the running turn
	}
	if errors.Is(err, host.ErrNoActiveTurn) {
		// The turn ended before the injection landed; send it as a new turn.
		if serr := a.sendUserTurnEntryHeld(e, text, images, withImages); serr != nil {
			return false, wireError(serr)
		}
		return true, nil
	}
	return false, wireError(err)
}

// sendUserTurn submits one user turn to the active session via the manager,
// maintaining the desktop-side turn bookkeeping: the in-flight mirror, the
// auto-title text, and the per-turn edit baseline reset.
func (a *App) sendUserTurn(sessionID, text string, images []llm.ImageSource, withImages bool) error {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	e, err := a.entryOf(sessionID)
	if err != nil {
		return err
	}
	if !e.questionMu.TryLock() {
		return host.ErrBusy
	}
	defer e.questionMu.Unlock()
	return a.sendUserTurnEntryHeld(e, text, images, withImages)
}

// Caller owns startMu and the entry's questionMu, including the injection fallback.
func (a *App) sendUserTurnEntryHeld(e *sessionEntry, text string, images []llm.ImageSource, withImages bool) error {
	if err := a.checkKnowledgeSubmission(e, nil); err != nil {
		return err
	}
	a.mu.Lock()
	provider, model := a.liveConfig.Provider, a.liveConfig.Model
	livePassport := a.livePassport
	a.mu.Unlock()
	// 只读字段,取到条目后脱锁用(见 sessionEntry 的并发约定)。
	id := e.id

	a.mu.Lock()
	prevText := e.lastUserText
	e.lastUserText = text
	e.turnActive = true
	a.mu.Unlock()

	debugLog("turn submit: provider=%s model=%s passport=%v withImages=%v textLen=%d", provider, model, livePassport, withImages, len(text))
	var err error
	if withImages {
		err = a.mgr.SendMessageWithImages(id, text, images)
	} else {
		err = a.mgr.SendMessage(id, text)
	}
	if err != nil {
		a.mu.Lock()
		e.lastUserText = prevText
		// A busy rejection means another turn is still running; any other
		// failure means nothing is in flight.
		e.turnActive = errors.Is(err, host.ErrBusy)
		a.mu.Unlock()
		debugLog("turn submit rejected: %v", err)
		return err
	}
	return nil
}

// Interrupt cancels the in-flight turn and denies any pending approval prompts.
func (a *App) Interrupt(sessionID string) error {
	id, err := a.sessionIDOf(sessionID)
	if err != nil {
		return nil // interrupting nothing is a no-op (pre-host behavior)
	}
	// Keep cancellation and the current host interrupt in one publication fence.
	// A delayed Stop must not cancel a subsequent admission after this one exits.
	a.mu.Lock()
	if e := a.entryLocked(id); e != nil && e.pendingQuestion != nil {
		e.pendingQuestion.cancel()
	}
	if e := a.entryLocked(id); e != nil && e.chat != nil {
		// receipt 写盘中的知识库提交尚无 run，也要留下停止代际，防止随后启动。
		e.chat.stopEpoch++
		if e.chat.run != nil {
			e.chat.run.cancel()
		}
		a.mu.Unlock()
		return nil
	}
	interruptErr := a.mgr.Interrupt(id)
	a.mu.Unlock()
	if err := interruptErr; err != nil && !errors.Is(err, host.ErrSessionNotFound) {
		return wireError(err)
	}
	return nil
}

// ResolvePermission delivers the user's decision for a pending approval request.
// 会话必须显式指定的理由在 entryOf 的注释里:并行之后按"当前是哪条"去解,会把
// B 会话的授权解到 A 头上。
func (a *App) ResolvePermission(sessionID, id, decision string) error {
	sid, err := a.sessionIDOf(sessionID)
	if err != nil {
		return wireError(err)
	}
	return wireError(a.mgr.ResolvePermission(sid, id, decision))
}

// Compact summarizes the oldest turns now and reports the message counts.
func (a *App) Compact(sessionID string) (CompactResult, error) {
	id, err := a.sessionIDOf(sessionID)
	if err != nil {
		return CompactResult{}, wireError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var result CompactResult
	err = a.mgr.WithIdleSession(ctx, id, func(ctx context.Context, _ host.Session, _ sessions.Backend) error {
		session, err := a.engineSessionOf(id)
		if err != nil {
			return err
		}
		before, after, usage, err := session.Compact(ctx)
		if err != nil {
			return err
		}
		result = CompactResult{Before: before, After: after, ContextTokens: session.EstimateContextTokens(), InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
		return nil
	})
	return result, wireError(err)
}

// The host admits the turn before this callback, but cannot execute tools until
// it returns. Do not reset edit baselines after an asynchronous receipt arrives.
func (a *App) onTurnStart(sessionID string) {
	// Host installs turnCancel before this synchronous hook and launches RunQuestion
	// only afterward. Bridge a Stop that raced between validation and registration
	// to that newly registered turn before it can persist input or invoke a model.
	a.mu.Lock()
	cancelled := false
	if e := a.entryLocked(sessionID); e != nil && e.pendingQuestion != nil && e.pendingQuestion.ctx.Err() != nil {
		_ = a.mgr.Interrupt(sessionID)
		cancelled = true
	}
	a.mu.Unlock()
	if cancelled {
		return
	}
	e, err := a.entryOf(sessionID)
	if err != nil {
		return
	}
	e.edits.BeginTurn()
	if e.plans != nil {
		e.plans.NoteUserTurn()
	}
	// Freeze callable libraries before the first model/tool step (no retrieve).
	a.freezeKnowledgeTurn(e, "")
	// A fresh TurnID and image generation target per independent turn; injected
	// messages never reach this hook, so they keep the running turn's budget.
	a.freezeImageGenerationTurn(e)
}
