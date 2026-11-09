package desktop

import "context"

// questionAdmission covers validation before the engine owns a turn. Its
// identity remains published until SubmitQuestion returns, including the
// synchronous host OnTurnStart handoff. All pointer access uses App.mu.
type questionAdmission struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (a *App) beginQuestionAdmission(ctx context.Context, e *sessionEntry, cancel context.CancelFunc) func() {
	pending := &questionAdmission{ctx: ctx, cancel: cancel}
	a.mu.Lock()
	e.pendingQuestion = pending
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		if e.pendingQuestion == pending {
			e.pendingQuestion = nil
		}
		a.mu.Unlock()
		cancel()
	}
}

// Logout cancels only pending platform admissions, not unrelated custom chats.
// Cancelling contexts is nonblocking and never waits for startMu/questionMu.
func (a *App) cancelQuestionAdmissions() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.sessions {
		if e.chat != nil && e.chat.run != nil {
			e.chat.run.cancel()
		}
		if e.passport && e.pendingQuestion != nil {
			e.pendingQuestion.cancel()
		}
	}
}
