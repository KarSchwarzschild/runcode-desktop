package desktop

import "testing"

func TestKnowledgeHistoryEligibilityByTurnOutcome(t *testing.T) {
	state := newKnowledgeTurnState("sess-elig")
	state.recordSources("q-ok", []KnowledgeSource{{ID: "kb:1", LibraryID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DocumentID: "d", ChunkID: "c", Title: "t"}})
	state.recordSources("q-stop", []KnowledgeSource{{ID: "kb:2", LibraryID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DocumentID: "d2", ChunkID: "c2", Title: "t2"}})
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
