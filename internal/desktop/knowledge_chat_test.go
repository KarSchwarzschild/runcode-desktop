package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kc "github.com/wt68/runcode/internal/knowledgechat"
)

func TestKnowledgeOriginalTextAndSelection(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var body struct {
		LibraryIDs []string `json:"libraryIds"`
		Question   string   `json:"question"`
	}
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"没有匹配资料\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/retrieve") {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprintf(w, `{"status":"empty","libraries":[{"id":%q,"title":"库"}],"hits":[]}`, knowledgeTestID)
			return
		}
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(1)
	original := " 原问\n保持😀 "
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, SubmissionID: "one", Text: original, KnowledgeRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("retrieve not started")
	}
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: info.SessionID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	close(release)
	d := waitKnowledgeTerminal(t, app, info.SessionID)
	if body.Question != original || len(body.LibraryIDs) != 1 || body.LibraryIDs[0] != knowledgeTestID || d.Questions[0].Text != original || len(d.Questions[0].Selection.LibraryIDs) != 1 || len(d.Selection.LibraryIDs) != 0 {
		t.Fatal("in-flight selection or original changed")
	}
	// 零命中仍可调用已授权模型，但不能伪造来源；中途清空标签不改变该题快照。
	if d.Questions[0].Status != kc.Completed || d.Questions[0].Answer != "没有匹配资料" || len(d.Questions[0].Sources) != 0 || len(d.Questions[0].AffectedLibraryIDs) != 0 {
		t.Fatal("零命中回答或来源不符合约定", d.Questions[0].Status)
	}
}

func TestKnowledgeSelectionReturnsValidatedSummaries(t *testing.T) {
	var details atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		details.Add(1)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	view, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: info.SessionID, ExpectedRevision: 1, LibraryIDs: []string{knowledgeTestID}})
	if err != nil || len(view.Libraries) != 1 || view.Libraries[0].UnavailableReason != "" || view.Libraries[0].Title != "Authorized library" || details.Load() != 1 {
		t.Fatalf("已验证的选库不能发送或重复读详情: %+v %v details=%d", view, err, details.Load())
	}
}

func TestKnowledgeChatRejectsFailedAcceptanceBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := app.entryOf(info.SessionID)
	path := filepath.Join(e.workspace, ".runcode", "knowledge-chat", e.id+".json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	rev := int64(1)
	if receipt, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: e.id, SubmissionID: "one", Text: "unsent", KnowledgeRevision: &rev}); err == nil || receipt.QuestionID != "" {
		t.Fatal("corrupt store accepted", err)
	}
	if calls.Load() != before {
		t.Fatal("HTTP preceded durable acceptance")
	}
}

func TestKnowledgePreparationHTTPFailures(t *testing.T) {
	for _, status := range []int{403, 404, 502, 503, 504, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var malformed atomic.Bool
			app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/retrieve") {
					w.WriteHeader(status)
					if malformed.Load() {
						fmt.Fprint(w, `{"status":`)
					} else {
						fmt.Fprint(w, "private upstream payload")
					}
					return
				}
				fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
			})
			defer app.CloseAllSessions()
			info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
			if err != nil {
				t.Fatal(err)
			}
			e, _ := app.entryOf(info.SessionID)
			malformed.Store(true)
			deps := app.knowledgeChatDependencies(e)
			var modelCalls atomic.Int32
			deps.AuthorizeModel = func(context.Context, kc.Scope, string) error { modelCalls.Add(1); return nil }
			_, err = kc.Prepare(context.Background(), e.chat.scope, kc.RequestConfig{Model: "fixture", Window: 8192}, kc.Question{Text: "question", Selection: kc.Selection{LibraryIDs: []string{knowledgeTestID}}}, nil, deps)
			if err == nil || modelCalls.Load() != 0 || strings.Contains(err.Error(), "private upstream") || status == 503 && strings.Contains(err.Error(), "登录") {
				t.Fatal("upstream failure misclassified or reached model", err)
			}
		})
	}
}

func TestKnowledgeChatsPrepareConcurrentlyAndLogoutCancels(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/retrieve") {
			_, _ = io.Copy(io.Discard, r.Body)
			entered <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	ids := []string{}
	for i := 0; i < 2; i++ {
		info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.SessionID)
	}
	rev := int64(1)
	for _, id := range ids {
		if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, SubmissionID: "one", Text: "q", KnowledgeRevision: &rev}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("sessions serialized network requests")
		}
	}
	app.PassportLogout()
	for _, id := range ids {
		d := waitKnowledgeTerminal(t, app, id)
		if d.Questions[0].Status != kc.Cancelled {
			t.Fatal("logout did not cancel")
		}
		if _, err := app.GetQuestion(QuestionReference{SessionID: id, QuestionID: d.Questions[0].ID}); err == nil {
			t.Fatal("logged-out history visible")
		}
	}
}

func TestKnowledgeChatIdentityCheckToleratesTokenReadContention(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := app.entryOf(info.SessionID)
	app.tokens.mu.Lock()
	released := make(chan struct{})
	go func() { time.Sleep(20 * time.Millisecond); app.tokens.mu.Unlock(); close(released) }()
	err = app.checkKnowledgeChat(context.Background(), e)
	<-released
	if err != nil {
		t.Fatalf("short token lock contention rejected a valid account: %v", err)
	}
}

func knowledgeChatFixture(t *testing.T, handler http.HandlerFunc) (*App, string) {
	t.Helper()
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`)
			return
		}
		handler(w, r)
	})
	app.mu.Lock()
	e := app.entryLocked(id)
	e.passport = true
	e.connectionConfig.Provider = "openai"
	e.connectionConfig.BaseURL = passportConfig().BridgeBaseURL + "/t/tenant-A/v1"
	e.connectionConfig.Model = "fixture"
	e.connectionConfig.MaxContextTokens = 8192
	e.connectionConfig.MaxTokens = 512
	app.mu.Unlock()
	return app, id
}

func TestKnowledgeSessionRouting(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var retrieves atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/retrieve") {
			if retrieves.Add(1) == 1 {
				close(entered)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprintf(w, `{"status":"empty","libraries":[{"id":%q,"title":"库"}],"hits":[]}`, knowledgeTestID)
			return
		}
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	before := len(app.mgr.List())
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "knowledge" || len(app.mgr.List()) != before {
		t.Fatal("knowledge creation reached engine")
	}
	v, err := app.GetSessionKnowledge(info.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	req := SubmitQuestionRequest{SessionID: info.SessionID, Text: "原问", SubmissionID: "same", KnowledgeRevision: &v.Revision}
	one, err := app.SubmitQuestion(req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	two, err := app.SubmitQuestion(req)
	if err != nil || one.QuestionID != two.QuestionID {
		t.Fatal("repeat submission not idempotent", err)
	}
	if err := app.SendMessage(info.SessionID, "bypass"); err == nil {
		t.Fatal("legacy send accepted")
	}
	if _, err := app.InjectMessage(info.SessionID, "bypass"); err == nil {
		t.Fatal("steering accepted")
	}
	if _, err := app.ForkQuestion(QuestionReference{SessionID: info.SessionID, QuestionID: one.QuestionID}); err == nil {
		t.Fatal("host fork fell into engine")
	}
	if err := app.SetModel(info.SessionID, "other"); err == nil {
		t.Fatal("model destination changed")
	}
	req.SubmissionID = "different"
	if _, err := app.SubmitQuestion(req); err == nil {
		t.Fatal("busy not enforced")
	}
	if err := app.Interrupt(info.SessionID); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitKnowledgeTerminal(t, app, info.SessionID)
	if retrieves.Load() != 1 || len(app.mgr.List()) != before {
		t.Fatal("duplicate execution/engine route")
	}
	if err := app.CloseSession(info.SessionID); err != nil {
		t.Fatal(err)
	}
	resumed, err := app.ResumeSession(info.SessionID)
	if err != nil || resumed.Info.Kind != "knowledge" || len(app.mgr.List()) != before {
		t.Fatal("restore entered engine", err)
	}
}

func waitKnowledgeTerminal(t *testing.T, app *App, id string) kc.Document {
	t.Helper()
	e, err := app.entryOf(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d, err := e.chat.store.Load(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		app.mu.Lock()
		busy := e.turnActive
		app.mu.Unlock()
		if len(d.Questions) > 0 && kc.Terminal(d.Questions[len(d.Questions)-1].Status) && !busy {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("knowledge worker remained busy")
	return kc.Document{}
}
