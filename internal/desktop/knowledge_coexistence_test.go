package desktop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

// scriptedProvider drives deterministic ReAct rounds for public-API coexistence checks.
type scriptedProvider struct {
	mu       sync.Mutex
	requests []llm.Request
	steps    []scriptedStep
	idx      int
}

type scriptedStep struct {
	kind  string // text | tool
	text  string
	name  string
	id    string
	input string
}

func (p *scriptedProvider) Name() string { return "desktop-knowledge-coexistence" }
func (p *scriptedProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}
func (p *scriptedProvider) Stream(_ context.Context, req llm.Request) (llm.Stream, error) {
	p.mu.Lock()
	p.requests = append(p.requests, cloneLLMRequest(req))
	i := p.idx
	if i >= len(p.steps) {
		i = len(p.steps) - 1
	}
	step := p.steps[i]
	p.idx++
	p.mu.Unlock()
	ch := make(chan llm.StreamEvent, 8)
	switch step.kind {
	case "tool":
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStart, Block: &llm.ContentBlock{Type: llm.ContentBlockTypeToolUse, ID: step.id, Name: step.name}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockDelta, Delta: &llm.ContentDelta{InputJSON: []byte(step.input)}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeMessageStop, StopReason: llm.StopReasonToolUse, Usage: &llm.Usage{OutputTokens: 1}}
	default:
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStart, Block: &llm.ContentBlock{Type: llm.ContentBlockTypeText, Text: step.text}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeMessageStop, StopReason: llm.StopReasonEndTurn, Usage: &llm.Usage{OutputTokens: 2}}
	}
	close(ch)
	return scriptedStream{ch}, nil
}

type scriptedStream struct{ ch <-chan llm.StreamEvent }

func (s scriptedStream) Events() <-chan llm.StreamEvent { return s.ch }
func (scriptedStream) Err() error                       { return nil }
func (scriptedStream) Close() error                     { return nil }

func cloneLLMRequest(req llm.Request) llm.Request {
	out := req
	if req.Tools != nil {
		out.Tools = append([]llm.ToolSpec(nil), req.Tools...)
	}
	if req.Messages != nil {
		out.Messages = append([]llm.Message(nil), req.Messages...)
	}
	if req.System != nil {
		out.System = append([]llm.ContentBlock(nil), req.System...)
	}
	return out
}

type stubKnowledgeSearch struct {
	desc      string
	calls     atomic.Int32
	lastQuery string
}

func (t *stubKnowledgeSearch) Name() string { return "knowledge_search" }
func (t *stubKnowledgeSearch) Description() string {
	return t.desc
}
func (t *stubKnowledgeSearch) InputSchema() tool.Schema {
	return tool.Schema{
		Type:                 tool.SchemaTypeObject,
		AdditionalProperties: false,
		Required:             []string{"query"},
		Properties: map[string]tool.Schema{
			"query": {Type: tool.SchemaTypeString, Description: "检索问题"},
		},
	}
}
func (t *stubKnowledgeSearch) IsConcurrencySafe() bool { return false }
func (t *stubKnowledgeSearch) Run(_ context.Context, input json.RawMessage, _ *tool.Context, _ chan<- tool.Event) (tool.Result, error) {
	var body struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(input, &body)
	t.lastQuery = body.Query
	t.calls.Add(1)
	return tool.Result{Content: []tool.ResultContent{{Type: tool.ResultContentTypeText, Text: "slice: 合成命中"}}}, nil
}

type stubOARead struct{ calls atomic.Int32 }

func (t *stubOARead) Name() string             { return "oa_todo_list" }
func (t *stubOARead) Description() string      { return "列出当前用户 OA 待办（测试桩）" }
func (t *stubOARead) InputSchema() tool.Schema {
	return tool.Schema{Type: tool.SchemaTypeObject, Properties: map[string]tool.Schema{}}
}
func (t *stubOARead) IsConcurrencySafe() bool  { return false }
func (t *stubOARead) Run(context.Context, json.RawMessage, *tool.Context, chan<- tool.Event) (tool.Result, error) {
	t.calls.Add(1)
	return tool.Result{Content: []tool.ResultContent{{Type: tool.ResultContentTypeText, Text: "todo:ok"}}}, nil
}

var coexistenceProviderOnce sync.Once
var coexistenceActive = &scriptedProvider{}

func TestKnowledgeCoexistencePublicAPI(t *testing.T) {
	isolateConfigDir(t)
	asTestBuild(t)
	coexistenceProviderOnce.Do(func() {
		llm.Register("desktop-knowledge-coexistence", func(llm.Config) (llm.Provider, error) { return coexistenceActive, nil })
	})

	kb := &stubKnowledgeSearch{desc: "查询已选知识库。可用库：demo:policies 制度指南（测试示例）"}
	oa := &stubOARead{}
	classes := map[string]permissions.ToolClass{
		"knowledge_search": permissions.ClassReadOnly,
		"oa_todo_list":     permissions.ClassReadOnly,
	}

	t.Run("directAnswerNoRetrieval", func(t *testing.T) {
		resetScriptedProvider(coexistenceActive, []scriptedStep{{kind: "text", text: "普通直接回答"}})
		cfg := engine.Config{
			CWD: t.TempDir(), SessionID: "coexist-direct", Provider: "desktop-knowledge-coexistence",
			Model: "fake", PermissionMode: "safe", PersistSession: true, MaxIterations: 3,
		}
		var toolNames []string
		opts := engine.Options{
			ExtraTools:           []tool.Tool{kb, oa},
			ToolClasses:          classes,
			OmitRequestSnapshots: true,
			LLMRequestObserver: func(_, _ string, req llm.Request) {
				for _, spec := range req.Tools {
					toolNames = append(toolNames, spec.Name)
					if spec.Name == "knowledge_search" && !strings.Contains(spec.Description, "demo:policies") {
						t.Errorf("missing library overview in tool description: %q", spec.Description)
					}
				}
			},
		}
		sess, err := engine.Build(cfg, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close(context.Background())
		result, err := sess.RunTurn(context.Background(), "不查库，直接答")
		if err != nil {
			t.Fatal(err)
		}
		if llm.TextContent(result.FinalAssistant) != "普通直接回答" {
			t.Fatalf("direct answer: %+v", result)
		}
		if kb.calls.Load() != 0 || oa.calls.Load() != 0 {
			t.Fatalf("direct answer must not call tools: kb=%d oa=%d", kb.calls.Load(), oa.calls.Load())
		}
		if !containsName(toolNames, "knowledge_search") || !containsName(toolNames, "oa_todo_list") {
			t.Fatalf("first request missing tools: %v", toolNames)
		}
	})

	t.Run("toolCallThenAnswerPersists", func(t *testing.T) {
		kb.calls.Store(0)
		oa.calls.Store(0)
		resetScriptedProvider(coexistenceActive, []scriptedStep{
			{kind: "tool", name: "knowledge_search", id: "kb-1", input: `{"query":"制度"}`},
			{kind: "text", text: "根据工具结果回答"},
		})
		cwd := t.TempDir()
		cfg := engine.Config{
			CWD: cwd, SessionID: "coexist-tool", Provider: "desktop-knowledge-coexistence",
			Model: "fake", PermissionMode: "safe", PersistSession: true, MaxIterations: 4,
		}
		opts := engine.Options{
			ExtraTools:  []tool.Tool{kb, oa},
			ToolClasses: classes,
		}
		sess, err := engine.Build(cfg, opts)
		if err != nil {
			t.Fatal(err)
		}
		result, err := sess.RunTurn(context.Background(), "请查制度")
		if err != nil {
			t.Fatal(err)
		}
		if kb.calls.Load() != 1 {
			t.Fatalf("expected one knowledge_search, got %d", kb.calls.Load())
		}
		if kb.lastQuery != "制度" {
			t.Fatalf("query=%q", kb.lastQuery)
		}
		if llm.TextContent(result.FinalAssistant) != "根据工具结果回答" {
			t.Fatalf("tool answer: %+v", result)
		}
		if len(sess.History()) < 3 {
			t.Fatalf("history too short: %d", len(sess.History()))
		}
		_ = sess.Close(context.Background())
		cfg.Resume = cfg.SessionID
		reopened, err := engine.Build(cfg, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close(context.Background())
		if len(reopened.History()) < 3 {
			t.Fatalf("reopen lost tool history: %d", len(reopened.History()))
		}
	})
}

func TestKnowledgeToolPublicAPI(t *testing.T) {
	kb := &stubKnowledgeSearch{desc: "库 A"}
	if kb.Name() != "knowledge_search" || kb.Description() == "" || kb.IsConcurrencySafe() {
		t.Fatal("tool contract")
	}
	schema, err := json.Marshal(kb.InputSchema())
	if err != nil || !strings.Contains(string(schema), "query") {
		t.Fatalf("schema: %s %v", schema, err)
	}
	res, err := kb.Run(context.Background(), json.RawMessage(`{"query":"x"}`), &tool.Context{SessionID: "s", ToolUseID: "t1"}, nil)
	if err != nil || res.IsError || kb.calls.Load() != 1 || kb.lastQuery != "x" {
		t.Fatalf("run: %+v %v", res, err)
	}
}

func TestKnowledgeSelectedAllowsOrdinaryAgentTurn(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SendMessage(id, "同会话普通提问"); err != nil {
		t.Fatalf("selected libraries must not block ordinary send: %v", err)
	}
	waitStarted(t, sess, "coexist-send")
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: id, Text: "带 revision", KnowledgeRevision: &selected.Revision}); err == nil || !strings.Contains(err.Error(), "busy") {
		// 上一回合仍在跑时 busy 可接受；关键是不得出现“尚未就绪／选库拒绝”。
		if err != nil && strings.Contains(err.Error(), "尚未就绪") {
			t.Fatalf("selection gate returned: %v", err)
		}
	}
}

func resetScriptedProvider(p *scriptedProvider, steps []scriptedStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = nil
	p.steps = steps
	p.idx = 0
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
