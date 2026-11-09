package desktop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/sessions"
)

func TestKnowledgeRealGateMakesNoModelRequestOrJournalWrite(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "")
	var modelCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/knowledge-bases/") {
			fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"fixture","object":"model","owned_by":"fixture"}]}`)
			return
		}
		modelCalls.Add(1)
		http.Error(w, "unexpected model request", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	t.Setenv("RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL", server.URL)
	app := New(&recordingSink{})
	defer func() { _ = app.CloseAllSessions() }()
	ws := t.TempDir()
	info, err := app.StartSession(StartSessionRequest{CWD: ws, Provider: "openai", Model: "fixture", BaseURL: server.URL + "/v1", APIKey: "synthetic", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	app.tokens.setInMemory(tokenSet{AccessToken: knowledgeJWT("A"), Expiry: time.Now().Add(time.Hour)})
	app.mu.Lock()
	e := app.entryLocked(info.SessionID)
	e.passport, e.tenantID = true, "tenant-A"
	e.connectionConfig.BaseURL = server.URL + "/t/tenant-A/v1"
	app.bindKnowledgeLocked(e)
	app.mu.Unlock()
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: info.SessionID, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws, ".runcode", "sessions", info.SessionID+".jsonl")
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "same-session ordinary", KnowledgeRevision: &selected.Revision}); err != nil {
		t.Fatalf("real selection must allow ordinary submit: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if modelCalls.Load() == 0 && string(before) == string(after) {
		t.Fatal("ordinary submit with selection did not reach model/journal")
	}
}

func TestOrdinaryHistoryCompatibility(t *testing.T) {
	for _, kind := range []string{sessions.BackendJSONL, sessions.BackendSQLite} {
		for _, location := range []string{"journal", "checkpoint"} {
			t.Run(kind+"/"+location, func(t *testing.T) {
				ws := t.TempDir()
				// Similar words inside user text/tool data are not format markers.
				raw := `{"role":"user","content":[{"type":"text","text":"question_context_version context_update"},{"type":"tool_use","input":{"question_context":true}}]}`
				writeRawCompatibilityHistory(t, ws, "ordinary", kind, location, raw)
				if err := checkKnowledgeHistory(context.Background(), ws, "ordinary", kind); err != nil {
					t.Fatal(err)
				}
				// Another session's legacy record cannot quarantine this session.
				writeRawCompatibilityHistory(t, ws, "other", kind, location, `{"role":"user","question_context":{}}`)
				if err := checkKnowledgeHistory(context.Background(), ws, "ordinary", kind); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestLargeOrdinaryHistoryRemainsUsable(t *testing.T) {
	// Ordinary input permits several individually valid images in one message;
	// the aggregate journal record can exceed the checkpoint's 32 MiB limit.
	block := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + strings.Repeat("A", 9<<20) + `"}}`
	raw := `{"role":"user","content":[` + strings.Join([]string{block, block, block, block}, ",") + `]}`
	for _, kind := range []string{sessions.BackendJSONL, sessions.BackendSQLite} {
		t.Run(kind, func(t *testing.T) {
			ws := t.TempDir()
			writeRawCompatibilityHistory(t, ws, "ordinary", kind, "journal", raw)
			if err := checkKnowledgeHistory(context.Background(), ws, "ordinary", kind); err != nil {
				t.Fatalf("valid ordinary record quarantined: %v", err)
			}
		})
	}
}

func TestLegacyHistoryJSONDecodingParity(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		checkpoint bool
	}{
		{`{"role":"user","Question_Context_Version":1}`, false},
		{`{"role":"user","Question_Context":null}`, false},
		{`{"role":"user","CONTEXT_UPDATE":{}}`, false},
		{`{"role":"user","queſtion_context":{}}`, false},
		{`{"messages":[],"Messages":[{"role":"user","question_context":{}}]}`, true},
		{`{"messages":[{"role":"user","question_context":{}}],"messages":[]}`, true},
	} {
		if err := checkKnowledgeRaw(context.Background(), []byte(tc.raw), tc.checkpoint); err == nil {
			t.Errorf("extension format bypassed: %s", tc.raw)
		}
	}
}

func TestOrdinaryToolJSONNumbersRemainOpaque(t *testing.T) {
	// Tool input is RawMessage in the engine; valid JSON need not fit float64.
	raw := []byte(`{"role":"assistant","content":[{"type":"tool_use","input":{"value":1e1000}}]}`)
	if err := checkKnowledgeRaw(context.Background(), raw, false); err != nil {
		t.Fatalf("ordinary tool JSON rejected: %v", err)
	}
}

func TestKnowledgeHistoryHonorsCancelledAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkKnowledgeHistory(ctx, t.TempDir(), "new", sessions.BackendJSONL); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled validation continued: %v", err)
	}
}

func TestLegacyHistoryBlocksActiveEntrypoints(t *testing.T) {
	isolateConfigDir(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.CloseAllSessions() }()
	id, sess := openParallelSession(t, app, built)
	writeRawCompatibilityHistory(t, sess.cwd, id, sessions.BackendJSONL, "journal", `{"role":"user","id":"q","question_context_version":1}`)
	ref := QuestionReference{SessionID: id, QuestionID: "q"}
	for _, call := range []func() error{
		func() error { _, err := app.GetQuestion(ref); return err },
		func() error { _, err := app.ForkQuestion(ref); return err },
		func() error {
			_, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: "blocked"})
			return err
		},
		func() error { return app.SendMessage(id, "blocked") },
		func() error { _, err := app.InjectMessage(id, "blocked"); return err },
		func() error { return app.SendMessageWithImages(id, "blocked", nil) },
		func() error { _, err := app.InjectMessageWithImages(id, "blocked", nil); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "旧版知识库扩展") {
			t.Fatalf("missing history barrier: %v", err)
		}
	}
	select {
	case <-sess.started:
		t.Fatal("legacy history reached a turn")
	default:
	}
}

func TestKnowledgeRealSelectionAllowsOrdinarySubmission(t *testing.T) {
	var calls atomic.Int32
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	app.mu.Lock()
	e := app.entryLocked(id)
	e.passport = true
	e.connectionConfig.BaseURL = passportConfig().BridgeBaseURL + "/t/tenant-A/v1"
	app.mu.Unlock()
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{
		SessionID: id, Text: "blocked override", Source: &QuestionReference{SessionID: id, QuestionID: "q"},
		KnowledgeRevision: &selected.Revision, KnowledgeOverride: &KnowledgeOverride{},
	}); err == nil || !strings.Contains(err.Error(), "尚未就绪") {
		t.Fatalf("override must remain rejected: %v", err)
	}
	if err := app.SendMessage(id, "ordinary with selection"); err != nil {
		t.Fatalf("selection must allow ordinary send: %v", err)
	}
}

// Inject raw bytes because the fixed engine's llm.Message silently discards
// these fields. Both journal and checkpoint must be checked before Build.
func TestLegacyKnowledgeHistoryRejectedBeforeBuild(t *testing.T) {
	for _, kind := range []string{sessions.BackendJSONL, sessions.BackendSQLite} {
		for _, location := range []string{"journal", "checkpoint"} {
			for _, raw := range []string{
				`{"role":"user","question_context_version":1,"content":[]}`,
				`{"role":"user","question_context":null,"content":[]}`,
				`{"role":"context_update","context_update":{},"content":[]}`,
				`{"role":"user","question_context_version":`,
			} {
				t.Run(kind+"/"+location+"/"+raw, func(t *testing.T) {
					isolateConfigDir(t)
					ws, id := t.TempDir(), "legacy"
					writeRawCompatibilityHistory(t, ws, id, kind, location, raw)
					app, _, built := newParallelApp(t)
					defer func() { _ = app.CloseAllSessions() }()
					_, err := app.buildSessionResolvedHeld(context.Background(), engine.Config{CWD: ws, Model: "fake", Resume: id, SessionBackend: kind}, false, "", false, false)
					if err == nil || !strings.Contains(err.Error(), "历史") {
						t.Fatalf("unsafe history was accepted: %v", err)
					}
					select {
					case <-built:
						t.Fatal("old history reached engine Build")
					default:
					}
				})
			}
		}
	}
}

func writeRawCompatibilityHistory(t *testing.T, ws, id, kind, location, raw string) {
	t.Helper()
	cp := fmt.Sprintf("cp-%x.json", sha256.Sum256([]byte(id)))
	if location == "checkpoint" {
		raw = `{"messages":[` + raw + `]}`
	}
	if kind == sessions.BackendJSONL {
		path := filepath.Join(ws, ".runcode", "sessions", id+".jsonl")
		if location == "checkpoint" {
			path = filepath.Join(ws, ".runcode", "context-state", cp)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	backend, err := sessions.OpenBackend(ws, kind)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(ws, ".runcode", "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if location == "checkpoint" {
		_, err = db.Exec("INSERT INTO context_state(key,data,created) VALUES(?,?,0)", cp, []byte(raw))
	} else {
		_, err = db.Exec("INSERT INTO messages(session_id,seq,role,data) VALUES(?,0,'user',?)", id, []byte(raw))
	}
	if err != nil {
		t.Fatal(err)
	}
}
