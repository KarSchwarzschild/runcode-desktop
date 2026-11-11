package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A corrupt sidecar must never be interpreted as an empty selection and sent.
func TestKnowledgeCorruptBlocksAllInputEntrypoints(t *testing.T) {
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	dir := filepath.Join(sess.cwd, ".runcode", "knowledge")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		send func() error
	}{
		{"submit", func() error {
			_, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: "原问保持不变"})
			return err
		}},
		{"send", func() error { return app.SendMessage(id, "原问保持不变") }},
		{"steering", func() error { _, err := app.InjectMessage(id, "原问保持不变"); return err }},
		{"send-images", func() error { return app.SendMessageWithImages(id, "原问保持不变", nil) }},
		{"steering-images", func() error { _, err := app.InjectMessageWithImages(id, "原问保持不变", nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.send(); err == nil || !strings.Contains(err.Error(), "知识库") {
				t.Fatalf("expected knowledge storage rejection, got %v", err)
			}
		})
	}
	select {
	case text := <-sess.started:
		t.Fatalf("corrupt selection reached model: %q", text)
	default:
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if e := app.entryLocked(id); e.lastUserText != "" || e.turnActive {
		t.Fatal("rejected input changed turn bookkeeping")
	}
}

// KB-UI-002：已选库后同会话普通发送应进入 agent；仅 KnowledgeOverride／陈旧 revision 仍拒绝。
func TestKnowledgeSelectedAllowsOrdinaryTurnUntilCleared(t *testing.T) {
	enableKnowledgeDemo(t)
	for _, key := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME", "USERPROFILE"} {
		t.Setenv(key, t.TempDir())
	}
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		mu.Lock()
		requests = append(requests, string(body.Messages))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"synthetic answer\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	sink := &recordingSink{}
	app := New(sink)
	defer func() { _ = app.CloseAllSessions() }()
	ws := t.TempDir()
	info, err := app.StartSession(StartSessionRequest{CWD: ws, Provider: "openai", Model: "knowledge-fixture", BaseURL: server.URL + "/v1", APIKey: "synthetic", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	id := info.SessionID
	// A real selected Passport tenant must survive a later direct-model switch.
	app.mu.Lock()
	app.entryLocked(id).tenantID = "synthetic-knowledge-tenant"
	app.bindKnowledgeLocked(app.entryLocked(id))
	app.mu.Unlock()
	first, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: "ordinary initial question"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 1, time.Now().Add(10*time.Second))
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetModel(id, "knowledge-fixture-2"); err != nil {
		t.Fatal(err)
	}
	rebuiltSelection, err := app.GetSessionKnowledge(id)
	if err != nil || rebuiltSelection.Revision != 1 || len(rebuiltSelection.Libraries) != 1 {
		t.Fatalf("model rebuild lost confirmed selection: %+v %v", rebuiltSelection, err)
	}
	if err := updateRawConfig(func(cfg *desktopConfig) error {
		cfg.CustomModels = []CustomModel{{Name: "different-endpoint", Provider: "openai", Model: "custom-model", BaseURL: server.URL + "/different-endpoint/v1", APIKey: "synthetic"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SwitchModel(id, "custom", "different-endpoint"); err != nil {
		t.Fatal(err)
	}
	customSelection, err := app.GetSessionKnowledge(id)
	if err != nil || customSelection.Revision != 1 || len(customSelection.Libraries) != 1 {
		t.Fatalf("different custom URL detached selection: %+v %v", customSelection, err)
	}
	// Model routing is independent of the knowledge service's account/tenant
	// scope. Platform → custom must preserve it too.
	app.mu.Lock()
	app.entryLocked(id).passport = true
	app.mu.Unlock()
	if _, err := app.SwitchModel(id, "custom", "different-endpoint"); err != nil {
		t.Fatal(err)
	}
	platformToCustom, err := app.GetSessionKnowledge(id)
	if err != nil || platformToCustom.Revision != 1 || len(platformToCustom.Libraries) != 1 {
		t.Fatalf("platform to custom detached selection: %+v %v", platformToCustom, err)
	}
	historyPath := filepath.Join(ws, ".runcode", "sessions", id+".jsonl")
	before, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(ws, "test.png")
	if err := os.WriteFile(image, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{
		SessionID: id, Text: "override", Source: &QuestionReference{SessionID: id, QuestionID: first.QuestionID},
		KnowledgeRevision: &selected.Revision, KnowledgeOverride: &KnowledgeOverride{},
	}); err == nil || !strings.Contains(err.Error(), "尚未就绪") {
		t.Fatalf("override must stay rejected: %v", err)
	}
	if err := app.SendMessage(id, "selected ordinary send"); err != nil {
		t.Fatalf("selected libraries must allow ordinary send: %v", err)
	}
	waitForTurns(t, sink, 2, time.Now().Add(10*time.Second))
	after, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("ordinary send with selection did not append history")
	}
	_ = image
	cleared, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: "stale revision", KnowledgeRevision: &selected.Revision}); err == nil {
		t.Fatal("stale frontend revision accepted")
	}
	original := "原始问题：保持换行\n不要改写。"
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: original, KnowledgeRevision: &cleared.Revision}); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 3, time.Now().Add(10*time.Second))
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, request := range requests {
		var messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if err := json.Unmarshal([]byte(request), &messages); err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if message.Role == "user" && message.Content == original {
				found = true
			}
		}
		if strings.Contains(request, "合成测试资料") || strings.Contains(request, "demo:employee") {
			t.Fatal("synthetic catalog was inserted into model request")
		}
	}
	if !found {
		t.Fatal("ordinary chat did not preserve the original user question")
	}
}
