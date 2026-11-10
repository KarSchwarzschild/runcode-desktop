package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kc "github.com/wt68/runcode/internal/knowledgechat"
	"github.com/wt68/runcode/internal/protocol"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

func TestKnowledgeInterruptWhileAcceptanceWaits(t *testing.T) {
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
	entered, release, updated := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(updated)
		_ = e.chat.store.Update(context.Background(), e.id, e.chat.scope, func(*kc.Document) error { close(entered); <-release; return nil })
	}()
	<-entered
	rev := int64(1)
	submitted := make(chan error, 1)
	go func() {
		_, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: e.id, Text: "q", SubmissionID: "stop-before-receipt", KnowledgeRevision: &rev})
		submitted <- err
	}()
	deadline := time.Now().Add(time.Second)
	for e.questionMu.TryLock() {
		e.questionMu.Unlock()
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("未开始准入")
		}
		time.Sleep(time.Millisecond)
	}
	before := calls.Load()
	if err := app.Interrupt(e.id); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	<-updated
	if err := <-submitted; err != nil {
		t.Fatal(err)
	}
	q := waitKnowledgeTerminal(t, app, e.id).Questions[0]
	if q.Status != kc.Cancelled || calls.Load() != before {
		t.Fatalf("停止后继续执行: status=%s extra=%d", q.Status, calls.Load()-before)
	}
}

func TestKnowledgeTerminalWaitsForIdentityContention(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := app.entryOf(info.SessionID)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &knowledgeRun{id: "event-run", question: "q", cancel: cancel, done: make(chan struct{})}
	app.mu.Lock()
	e.chat.run = run
	app.mu.Unlock()
	app.tokens.mu.Lock()
	done := make(chan struct{})
	go func() { app.emitKnowledgeChat(e, run, kc.Completed); close(done) }()
	// 模拟另一会话短暂续期持锁，已保存终态必须在身份可核对后补投递。
	time.Sleep(150 * time.Millisecond)
	app.tokens.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("终态未恢复投递")
	}
	app.mu.Lock()
	e.chat.run = nil
	app.mu.Unlock()
	if _, ok := app.out.(*recordingSink).lastOf(protocol.EventKnowledgeChat); !ok {
		t.Fatal("同账号锁争用导致终态被丢弃")
	}
}

func TestKnowledgeCancelDuringRealProvider(t *testing.T) {
	entered := make(chan struct{})
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分答案\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(1)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "q", SubmissionID: "cancel", KnowledgeRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入模型流")
	}
	if err := app.Interrupt(info.SessionID); err != nil {
		t.Fatal(err)
	}
	if q := waitKnowledgeTerminal(t, app, info.SessionID).Questions[0]; q.Status != kc.Cancelled {
		t.Fatal(q.Status)
	}
}

func TestKnowledgeTerminalSaveFailureNeverCompletes(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(1)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "q", SubmissionID: "save", KnowledgeRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入模型流")
	}
	e, _ := app.entryOf(info.SessionID)
	app.mu.Lock()
	run := e.chat.run
	app.mu.Unlock()
	// 用非文件目标模拟最终原子保存失败，验证模型成功不会提前宣告持久成功。
	path := filepath.Join(e.workspace, ".runcode", "knowledge-chat", e.id+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-run.done:
	case <-time.After(3 * time.Second):
		t.Fatal("保存失败未结束")
	}
	sink := app.out.(*recordingSink)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	found := false
	for _, event := range sink.events {
		if event.name != protocol.EventKnowledgeChat {
			continue
		}
		env := event.data.(hostproto.Envelope)
		p := env.Payload.(protocol.KnowledgeChatEvent)
		if p.Phase == kc.Completed {
			t.Fatal("终态写失败仍发送 completed")
		}
		if p.Phase == kc.Failed && strings.Contains(p.Error, "保存失败") {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少可恢复的保存失败提示")
	}
}

func TestKnowledgeProviderFailureAndRefresh(t *testing.T) {
	for _, scenario := range []string{"refresh", "unauthorized", "tool", "truncated", "broken", "limit"} {
		t.Run(scenario, func(t *testing.T) {
			var calls, refreshes atomic.Int32
			app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					t.Errorf("意外请求 %s", r.URL.Path)
					return
				}
				n := calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if scenario == "unauthorized" || scenario == "refresh" && n == 1 {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"private-error"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				switch scenario {
				case "tool":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"bad\",\"type\":\"function\",\"function\":{\"name\":\"shell\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
				case "truncated":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分答案\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
				case "broken":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"部分答案\"}}]}\n\ndata: {invalid-json\n\n")
				case "limit":
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":%q}}]}\n\n", strings.Repeat("x", 65537))
				default:
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"答案\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			})
			defer app.CloseAllSessions()
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				refreshes.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": knowledgeJWT("A"), "expires_in": 3600, "refresh_token": "synthetic"})
			}))
			defer tokens.Close()
			app.tokens.mu.Lock()
			app.tokens.tokenURL = tokens.URL
			app.tokens.ts.RefreshToken = "synthetic"
			app.tokens.ts.Expiry = time.Now().Add(time.Hour)
			app.tokens.mu.Unlock()
			info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
			if err != nil {
				t.Fatal(err)
			}
			rev := int64(1)
			if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "q", SubmissionID: "failure", KnowledgeRevision: &rev}); err != nil {
				t.Fatal(err)
			}
			q := waitKnowledgeTerminal(t, app, info.SessionID).Questions[0]
			wantCalls, wantRefresh := int32(1), int32(0)
			if scenario == "refresh" || scenario == "unauthorized" {
				wantCalls, wantRefresh = 2, 1
			}
			wantStatus := kc.Failed
			if scenario == "refresh" || scenario == "truncated" {
				wantStatus = kc.Completed
			}
			if calls.Load() != wantCalls || refreshes.Load() != wantRefresh || q.Status != wantStatus || q.Truncated != (scenario == "truncated") || strings.Contains(q.Error, "private-error") {
				t.Fatalf("scenario=%s calls=%d refresh=%d status=%s error=%s", scenario, calls.Load(), refreshes.Load(), q.Status, q.Error)
			}
		})
	}
}

// 使用已发布 Provider 的真实 HTTP/SSE 转换，验证资料没有经过引擎工具循环。
func TestKnowledgeProviderBoundary(t *testing.T) {
	var modelCalls, retrieves atomic.Int32
	var input map[string]any
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/retrieve"):
			retrieves.Add(1)
			fmt.Fprintf(w, `{"status":"matched","libraries":[{"id":%q,"title":"测试库"}],"hits":[{"libraryId":%q,"documentId":%q,"chunkId":"one","title":"测试文档","text":"KB-TEST-7421"}]}`, knowledgeTestID, knowledgeTestID, knowledgeTestID)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			modelCalls.Add(1)
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"答案 KB-TEST-7421\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":6}}\n\ndata: [DONE]\n\n")
		default:
			fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
		}
	})
	defer app.CloseAllSessions()
	before := len(app.mgr.List())
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(1)
	_, err = app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "事实是什么", SubmissionID: "provider", KnowledgeRevision: &rev})
	if err != nil {
		t.Fatal(err)
	}
	d := waitKnowledgeTerminal(t, app, info.SessionID)
	q := d.Questions[0]
	raw, _ := json.Marshal(input)
	if q.Status != kc.Completed || q.Answer != "答案 KB-TEST-7421" || len(q.Sources) != 1 || len(q.AffectedLibraryIDs) != 1 || modelCalls.Load() != 1 || retrieves.Load() != 1 || len(app.mgr.List()) != before {
		t.Fatalf("status=%s error=%s model=%d retrieve=%d", q.Status, q.Error, modelCalls.Load(), retrieves.Load())
	}
	if _, ok := input["tools"]; ok {
		t.Fatal("工具被发送")
	}
	if !strings.Contains(string(raw), "KB-TEST-7421") || q.Text != "事实是什么" {
		t.Fatal("资料或原问边界错误")
	}
	e, _ := app.entryOf(info.SessionID)
	path, _ := e.chat.store.Load(t.Context(), e.id)
	persisted, _ := json.Marshal(path)
	if strings.Contains(string(persisted), `"text":"KB-TEST-7421"`) {
		t.Fatal("检索正文落盘")
	}
}

func TestKnowledgeRefreshRechecksEmptySelectionPermissions(t *testing.T) {
	var calls, details atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/retrieve"):
			fmt.Fprintf(w, `{"status":"empty","libraries":[{"id":%q,"title":"库"}],"hits":[]}`, knowledgeTestID)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			calls.Add(1)
			w.WriteHeader(401)
		default:
			details.Add(1)
			if calls.Load() > 0 {
				w.WriteHeader(403)
				return
			}
			fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
		}
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(1)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "q", SubmissionID: "refresh-revoke", KnowledgeRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	q := waitKnowledgeTerminal(t, app, info.SessionID).Questions[0]
	if calls.Load() != 1 || details.Load() != 2 || q.Status != kc.Failed {
		t.Fatalf("零命中选库刷新后未重验: model=%d detail=%d status=%s", calls.Load(), details.Load(), q.Status)
	}
}

func TestKnowledgeRawSSEDumpRejected(t *testing.T) {
	var calls atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNCODE_SSE_DUMP_DIR", t.TempDir())
	rev := int64(1)
	before := calls.Load()
	receipt, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: info.SessionID, Text: "q", SubmissionID: "dump", KnowledgeRevision: &rev})
	if err == nil || receipt.QuestionID != "" || calls.Load() != before {
		t.Fatal("原始流抓包配置未在接受前拒绝")
	}
}
