package desktop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kc "github.com/wt68/runcode/internal/knowledgechat"
)

func savedKnowledgeChat(t *testing.T, app *App, source string) (*sessionEntry, kc.Document) {
	t.Helper()
	info, err := app.CreateKnowledgeChat(CreateKnowledgeChatRequest{SourceSessionID: source, LibraryIDs: []string{knowledgeTestID}})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := app.entryOf(info.SessionID)
	err = e.chat.store.Update(context.Background(), e.id, e.chat.scope, func(d *kc.Document) error {
		d.Questions = []kc.Question{{ID: "original", SubmissionID: "saved", Text: "原题", Status: kc.Completed, Answer: "本地保存的回答", Selection: d.Selection, Sources: []kc.Source{{ID: "S1", LibraryID: knowledgeTestID, DocumentID: "doc", ChunkID: "chunk", Title: "参考来源", SourceURL: "https://example.test/source"}}, AffectedLibraryIDs: []string{knowledgeTestID}, Truncated: true}}
		d.Selection = kc.Selection{Revision: 2, LibraryIDs: []string{}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.chat.store.Load(context.Background(), e.id)
	if err != nil {
		t.Fatal(err)
	}
	return e, d
}

func TestKnowledgeSessionLifecycle(t *testing.T) {
	var calls atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	before := calls.Load()
	// 服务切换后只展示本地已保存内容，不按旧账号会话偷偷恢复网络请求。
	t.Setenv("RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL", "http://127.0.0.1:1")
	if err := app.RenameKnowledgeChat(e.id, "保存的知识问答"); err != nil {
		t.Fatal(err)
	}
	list, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list {
		if item.ID == e.id {
			found = item.Kind == kc.Kind && item.Title == "保存的知识问答"
		}
	}
	if !found {
		t.Fatalf("列表遗漏宿主会话: %+v", list)
	}
	question, err := app.GetQuestion(QuestionReference{SessionID: e.id, QuestionID: "original"})
	if err != nil || question.Knowledge == nil || len(question.Knowledge.Libraries) != 1 || question.Knowledge.Libraries[0].ID != knowledgeTestID || len(question.Knowledge.Sources) != 1 {
		t.Fatalf("原题快照/来源丢失: %+v %v", question, err)
	}
	exported, err := app.ExportKnowledgeChat(e.id)
	if err != nil || !strings.Contains(exported, "本地保存的回答") || !strings.Contains(exported, "参考来源") {
		t.Fatalf("离线导出失败: %s %v", exported, err)
	}
	if err := app.CloseSession(e.id); err != nil {
		t.Fatal(err)
	}
	resumed, err := app.ResumeSession(e.id)
	if err != nil || len(resumed.Blocks) != 2 || resumed.Blocks[0].Knowledge == nil || len(resumed.Blocks[0].Knowledge.Sources) != 1 {
		t.Fatalf("离线恢复丢失问答/来源: %+v %v", resumed, err)
	}
	if calls.Load() != before {
		t.Fatal("恢复已保存问答访问网络")
	}
	if err := app.SetPermissionMode(e.id, "flight"); err == nil {
		t.Fatal("知识库会话进入工具授权设置")
	}
	if _, err := app.SetPlanMode(e.id, true); err == nil {
		t.Fatal("知识库会话进入工具计划模式")
	}
	if _, err := app.SetThinkingEffort(e.id, "high"); err == nil {
		t.Fatal("未支持的知识库设置进入引擎")
	}
	if _, err := app.SetReasoningScenario(e.id, "auto"); err == nil {
		t.Fatal("知识库会话进入引擎推理设置")
	}
	if len(app.mgr.List()) != 1 {
		t.Fatal("知识库生命周期新建了引擎会话")
	}
}

func TestKnowledgeSavedHistoryHiddenAfterAccountChange(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	app.tokens.setInMemory(tokenSet{AccessToken: knowledgeJWT("B"), Expiry: time.Now().Add(time.Hour)})
	if _, err := app.GetQuestion(QuestionReference{SessionID: e.id, QuestionID: "original"}); err == nil {
		t.Fatal("换账号仍显示原题")
	}
	if exported, err := app.ExportKnowledgeChat(e.id); err == nil || exported != "" {
		t.Fatal("换账号仍能导出正文")
	}
	if _, err := app.ResumeSession(e.id); err == nil {
		t.Fatal("换账号仍能恢复正文")
	}
	list, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == e.id {
			t.Fatal("换账号仍显示知识库标题预览")
		}
	}
	opened := app.OpenSessions()
	if len(opened) != 1 || opened[0].SessionID != source {
		t.Fatalf("换账号后打开列表仍暴露知识库条目或误隐藏普通会话: %+v", opened)
	}
}

func TestKnowledgeDeleteUsesEntryWorkspace(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, d := savedKnowledgeChat(t, app, source)
	other := d
	other.ID = "kc-other"
	other.Scope.Workspace = t.TempDir()
	otherStore := kc.NewStore(other.Scope.Workspace)
	if err := otherStore.Create(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := app.registerKnowledgeChat(other, e.connectionConfig, otherStore); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteSession(e.id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.chat.store.Load(context.Background(), e.id); !os.IsNotExist(err) {
		t.Fatalf("未删除目标工作区文件: %v", err)
	}
	if _, err := otherStore.Load(context.Background(), other.ID); err != nil {
		t.Fatalf("误删其他会话: %v", err)
	}
}

func TestKnowledgeForkRoutingAndRetryOverride(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, original := savedKnowledgeChat(t, app, source)
	ref := QuestionReference{SessionID: e.id, QuestionID: "original"}
	fork, err := app.ForkQuestion(ref)
	if err != nil || fork.Source == nil || *fork.Source != ref || fork.Info.SessionID == e.id || len(fork.Blocks) != 0 {
		t.Fatalf("宿主重试分支错误: %+v %v", fork, err)
	}
	if app.focusedSessionID() != e.id {
		t.Fatal("分支创建改变当前草稿的焦点")
	}
	child, _ := app.entryOf(fork.Info.SessionID)
	rev := int64(1)
	in, err := app.knowledgeChatSubmitInput(context.Background(), child, SubmitQuestionRequest{SessionID: child.id, SubmissionID: "retry", Source: &ref, Text: "修改重试", KnowledgeRevision: &rev, KnowledgeOverride: &KnowledgeOverride{LibraryIDs: []string{}}})
	if err != nil || in.LibraryIDs == nil || len(*in.LibraryIDs) != 0 || in.Parent == nil {
		t.Fatalf("重试空覆盖未保留: %+v %v", in, err)
	}
	bad := ref
	bad.QuestionID = "forged"
	if _, err := app.knowledgeChatSubmitInput(context.Background(), child, SubmitQuestionRequest{SessionID: child.id, Source: &bad, KnowledgeRevision: &rev}); err == nil {
		t.Fatal("伪造父题通过")
	}
	after, _ := e.chat.store.Load(context.Background(), e.id)
	if !reflect.DeepEqual(original, after) {
		t.Fatal("创建重试分支修改了原记录")
	}
}

func TestKnowledgeRecoveryNeverAutoRuns(t *testing.T) {
	var calls atomic.Int32
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	if err := e.chat.store.Update(context.Background(), e.id, e.chat.scope, func(d *kc.Document) error {
		d.Questions[0].Status = kc.Generating
		d.Questions[0].Answer = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseSession(e.id); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	resumed, err := app.ResumeSession(e.id)
	if err != nil || len(resumed.Blocks) != 1 || resumed.Blocks[0].Knowledge.Status != kc.Interrupted || calls.Load() != before {
		t.Fatalf("恢复自动执行或未标中断: %+v %v", resumed, err)
	}
}

func TestLegacyQuestionContextReadOnlyList(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := app.entryOf(source)
	dir := filepath.Join(e.workspace, ".runcode", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.jsonl"), []byte(`{"role":"user","content":[{"type":"text","text":"不能泄漏的旧知识正文"}],"question_context_version":1}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == "legacy" && strings.Contains(item.Title, "不能泄漏") {
			t.Fatal("旧扩展未知身份仍展示正文预览")
		}
	}
	if _, err := app.ResumeSession("legacy"); err == nil {
		t.Fatal("旧扩展记录进入引擎恢复")
	}
}

func TestKnowledgeRenamePreservesConcurrentTerminalSave(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	loaded, release, saved := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		saved <- e.chat.store.Update(context.Background(), e.id, e.chat.scope, func(d *kc.Document) error {
			close(loaded)
			<-release
			d.Questions[0].Answer = "并发终态保存的回答"
			return nil
		})
	}()
	<-loaded
	renamed := make(chan error, 1)
	go func() { renamed <- app.RenameKnowledgeChat(e.id, "并发重命名") }()
	select {
	case err := <-renamed:
		close(release)
		<-saved
		t.Fatalf("重命名绕过了运行期文档锁: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if err := <-renamed; err != nil {
		t.Fatal(err)
	}
	d, err := e.chat.store.Load(context.Background(), e.id)
	if err != nil || d.Title != "并发重命名" || d.Questions[0].Answer != "并发终态保存的回答" {
		t.Fatalf("标题或回答被另一写入覆盖: %+v %v", d, err)
	}
}

func TestKnowledgeCloseFencesAdmissionBeforeCancelWait(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	ctx, cancel := context.WithCancel(context.Background())
	fenceAtCancel := make(chan bool, 1)
	done := make(chan struct{})
	defer close(done)
	app.mu.Lock()
	e.chat.run = &knowledgeRun{id: "closing", question: "original", cancel: func() {
		// 所有取消入口均持 App.mu，此处记录取消发生时的准入状态。
		select {
		case fenceAtCancel <- e.closed:
		default:
		}
		cancel()
	}, done: done}
	e.turnActive = true
	app.mu.Unlock()
	closed := make(chan error, 1)
	go func() { closed <- app.CloseSession(e.id) }()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("关闭未取消运行")
	}
	fenced := <-fenceAtCancel
	if !fenced {
		t.Fatal("取消等待期间仍允许新的知识库准入")
	}
	rev := int64(2)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: e.id, Text: "不得接受", SubmissionID: "late", KnowledgeRevision: &rev}); err == nil {
		t.Fatal("关闭中仍接受新问题")
	}
}

func TestKnowledgeDeleteCancelsActiveRun(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/retrieve") {
			_, _ = io.Copy(io.Discard, r.Body)
			close(entered)
			select {
			case <-r.Context().Done():
				close(cancelled)
			case <-release:
			}
			return
		}
		fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
	})
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: e.id, ExpectedRevision: 2, LibraryIDs: []string{knowledgeTestID}}); err != nil {
		t.Fatal(err)
	}
	rev := int64(3)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: e.id, Text: "正在检索", SubmissionID: "active", KnowledgeRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入检索")
	}
	if err := app.DeleteSession(e.id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("删除未取消网络请求")
	}
	if _, err := e.chat.store.Load(context.Background(), e.id); !os.IsNotExist(err) {
		t.Fatalf("删除后重新出现宿主文件: %v", err)
	}
}

func TestKnowledgeDestinationSticky(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	// 即使移除了全部标签和可传递历史，kind 与原服务绑定仍然存在。
	if err := e.chat.store.Update(context.Background(), e.id, e.chat.scope, func(d *kc.Document) error { d.Questions = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SwitchModel(e.id, "custom", "external"); err == nil {
		t.Fatal("空标签知识库会话切到外部模型")
	}
	if err := app.SetModel(e.id, "external"); err == nil {
		t.Fatal("知识库历史裁剪后失去目标绑定")
	}
	t.Setenv("RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL", "http://127.0.0.1:1")
	rev := int64(2)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: e.id, SubmissionID: "changed-service", Text: "正文", KnowledgeRevision: &rev}); err == nil {
		t.Fatal("服务边界变化仍可发送")
	}
	d, err := e.chat.store.Load(context.Background(), e.id)
	if err != nil || len(d.Questions) != 0 {
		t.Fatal("服务边界失败仍持久化新准入", err)
	}
}

func TestKnowledgeClosePreservesAgentPreview(t *testing.T) {
	app, source := knowledgeChatFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID)) })
	defer app.CloseAllSessions()
	e, _ := savedKnowledgeChat(t, app, source)
	// 并行会话夹具替换了引擎创建，显式建立普通会话持有的真实预览引用。
	app.startPreview(e.workspace)
	app.mu.Lock()
	before := app.previews[e.workspace]
	app.mu.Unlock()
	if before == nil || before.refs != 1 {
		t.Fatal("普通会话预览未正常建立")
	}
	if err := app.CloseSession(e.id); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	after := app.previews[e.workspace]
	app.mu.Unlock()
	if after == nil || after.refs != 1 || after.url != before.url {
		t.Fatal("关闭知识库错误释放了普通会话预览引用")
	}
}
