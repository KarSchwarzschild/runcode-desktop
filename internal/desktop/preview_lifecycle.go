package desktop

import (
	"context"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"

	desktopproto "github.com/wt68/runcode/internal/protocol"
)

// emitPreviewLifecycle notifies the UI that file-preview caches for a session
// instance must change. Call while the entry's emit is still live (before it is
// cleared on close).
func (a *App) emitPreviewLifecycle(e *sessionEntry, phase, generation string) {
	if a == nil || e == nil {
		return
	}
	a.mu.Lock()
	emit := e.emit
	id := e.id
	a.mu.Unlock()
	emitPreviewLifecycleTo(emit, id, phase, generation)
}

func emitPreviewLifecycleTo(emit func(string, any), sessionID, phase, generation string) {
	if emit == nil || sessionID == "" {
		return
	}
	emit(desktopproto.EventPreviewLifecycle, desktopproto.PreviewLifecycle{
		SessionID:  sessionID,
		Phase:      phase,
		Generation: generation,
	})
}

// previewGenerationOf reads the live host generation for a session, or "" if
// unavailable. Used after a same-ID rebuild so ready events carry the real new
// generation rather than a guessed value.
func (a *App) previewGenerationOf(sessionID string) string {
	if a == nil || a.mgr == nil || sessionID == "" {
		return ""
	}
	var generation string
	_ = a.mgr.WithSessionOperation(context.Background(), sessionID, func(_ context.Context, access host.SessionAccess) error {
		generation = access.Generation
		return nil
	})
	return generation
}
