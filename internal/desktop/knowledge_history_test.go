package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeSessionJSONL(t *testing.T, workspace, id string, body []byte) {
	t.Helper()
	dir := filepath.Join(workspace, ".runcode", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeKnowledgeSidecar(t *testing.T, workspace, id string) {
	t.Helper()
	dir := filepath.Join(workspace, ".runcode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := knowledgeDisk{Version: knowledgeStoreVersionV1, SessionID: id, Selections: map[string]knowledgeSelection{}}
	raw, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func userLine(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"role": "user", "content": text})
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestCheckKnowledgeHistorySkipsSessionsWithoutKnowledge(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-plain"
	body := append(userLine(t, "hi"), []byte(`{"role":"user","cont`)...)
	writeSessionJSONL(t, workspace, id, body)
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); err != nil {
		t.Fatalf("plain session with torn tail: %v", err)
	}
}

func TestCheckKnowledgeHistoryToleratesTornTailWhenKnowledgeUsed(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-kb"
	writeKnowledgeSidecar(t, workspace, id)
	body := append(userLine(t, "hi"), []byte(`{"role":"user","cont`)...)
	writeSessionJSONL(t, workspace, id, body)
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); err != nil {
		t.Fatalf("knowledge session with torn tail: %v", err)
	}
}

func TestCheckKnowledgeHistoryRejectsTerminatedCorruptLine(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-kb-bad"
	writeKnowledgeSidecar(t, workspace, id)
	writeSessionJSONL(t, workspace, id, []byte("{not-json}\n"))
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); !errors.Is(err, errKnowledgeHistoryFormat) {
		t.Fatalf("err = %v, want format", err)
	}
}

func TestCheckKnowledgeHistoryRejectsLegacyFields(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-kb-legacy"
	writeKnowledgeSidecar(t, workspace, id)
	line, err := json.Marshal(map[string]any{"role": "user", "question_context": map[string]any{"x": 1}})
	if err != nil {
		t.Fatal(err)
	}
	writeSessionJSONL(t, workspace, id, append(line, '\n'))
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); !errors.Is(err, errLegacyKnowledgeHistory) {
		t.Fatalf("err = %v, want legacy", err)
	}
}

func TestCheckKnowledgeHistoryCachesUnchangedFingerprint(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-kb-cache"
	writeKnowledgeSidecar(t, workspace, id)
	writeSessionJSONL(t, workspace, id, userLine(t, "ok"))
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); err != nil {
		t.Fatal(err)
	}
	key := knowledgeHistoryCacheKey(workspace, id, "")
	first, ok := knowledgeHistoryCache.Load(key)
	if !ok {
		t.Fatal("expected cache entry")
	}
	if err := checkKnowledgeHistory(context.Background(), workspace, id, ""); err != nil {
		t.Fatal(err)
	}
	second, _ := knowledgeHistoryCache.Load(key)
	if first.(knowledgeHistoryCacheEntry).fp != second.(knowledgeHistoryCacheEntry).fp {
		t.Fatal("fingerprint changed without file edits")
	}
}
