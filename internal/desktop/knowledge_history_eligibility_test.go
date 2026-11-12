package desktop

import "testing"

func TestKnowledgeHistoryEligibilityByTurnOutcome(t *testing.T) {
	state := newKnowledgeTurnState("sess-elig")
	state.markOutcome("q-ok", "completed")
	state.markOutcome("q-stop", "cancelled")
	state.markOutcome("q-fail", "failed")

	if !state.knowledgeEligibleForReuse("q-ok") {
		t.Fatal("completed turn must be eligible")
	}
	if state.knowledgeEligibleForReuse("q-stop") || state.knowledgeEligibleForReuse("q-fail") {
		t.Fatal("stopped/failed turns must not auto-reuse")
	}
	if state.knowledgeEligibleForReuse("q-unknown") {
		t.Fatal("unknown outcome is not eligible")
	}
}
