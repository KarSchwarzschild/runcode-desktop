package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wt68/runcode/internal/knowledgetool"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

func TestKnowledgeToolDirectAnswerMakesNoRetrieval(t *testing.T) {
	var retrieves atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/retrieve") {
			retrieves.Add(1)
			w.WriteHeader(500)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(bridge.Close)

	lib := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	host := &countingRetrieveHost{hits: nil}
	tl := &knowledgetool.Tool{
		Host: host,
		SnapshotFunc: func() knowledgetool.Snapshot {
			return knowledgetool.Snapshot{Libraries: []knowledgetool.Overview{{ID: lib, Title: "制度"}}}
		},
	}
	tl.BeginQuestion("q-direct")

	provider := &scriptedProvider{steps: []scriptedStep{{kind: "text", text: "你好，无需查库"}}}
	coexistenceProviderOnce.Do(func() {
		llm.Register("desktop-knowledge-coexistence", func(llm.Config) (llm.Provider, error) { return coexistenceActive, nil })
	})
	resetScriptedProvider(coexistenceActive, provider.steps)

	cfg := engine.Config{
		CWD: t.TempDir(), SessionID: "kb-tool-direct", Provider: "desktop-knowledge-coexistence",
		Model: "fake", PermissionMode: "safe", PersistSession: true, MaxIterations: 2,
	}
	sess, err := engine.Build(cfg, engine.Options{
		ExtraTools:  []tool.Tool{tl},
		ToolClasses: map[string]permissions.ToolClass{knowledgetool.Name: permissions.ClassReadOnly},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close(context.Background())
	if _, err := sess.RunTurn(context.Background(), "你好"); err != nil {
		t.Fatal(err)
	}
	if host.calls.Load() != 0 || retrieves.Load() != 0 {
		t.Fatalf("direct answer must not retrieve: host=%d bridge=%d", host.calls.Load(), retrieves.Load())
	}
}

func TestKnowledgeToolCallPrecedesRetrieval(t *testing.T) {
	lib := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	host := &countingRetrieveHost{hits: []knowledgetool.RawHit{{
		LibraryID: lib, DocumentID: "doc-1", ChunkID: "c1", Title: "条款", Text: "第一条 合成正文",
	}}}
	tl := &knowledgetool.Tool{
		Host: host,
		SnapshotFunc: func() knowledgetool.Snapshot {
			return knowledgetool.Snapshot{Libraries: []knowledgetool.Overview{{ID: lib, Title: "制度"}}}
		},
	}
	tl.BeginQuestion("q-tool")
	resetScriptedProvider(coexistenceActive, []scriptedStep{
		{kind: "tool", name: knowledgetool.Name, id: "kb-1", input: `{"query":"第一条"}`},
		{kind: "text", text: "依据资料回答"},
	})
	cfg := engine.Config{
		CWD: t.TempDir(), SessionID: "kb-tool-call", Provider: "desktop-knowledge-coexistence",
		Model: "fake", PermissionMode: "safe", PersistSession: true, MaxIterations: 4,
	}
	var sawToolDef bool
	sess, err := engine.Build(cfg, engine.Options{
		ExtraTools:  []tool.Tool{tl},
		ToolClasses: map[string]permissions.ToolClass{knowledgetool.Name: permissions.ClassReadOnly},
		LLMRequestObserver: func(_, _ string, req llm.Request) {
			for _, spec := range req.Tools {
				if spec.Name == knowledgetool.Name {
					sawToolDef = true
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close(context.Background())
	result, err := sess.RunTurn(context.Background(), "查制度第一条")
	if err != nil {
		t.Fatal(err)
	}
	if !sawToolDef {
		t.Fatal("first model request missing knowledge_search")
	}
	if host.calls.Load() != 1 {
		t.Fatalf("retrieve calls=%d", host.calls.Load())
	}
	if llm.TextContent(result.FinalAssistant) != "依据资料回答" {
		t.Fatalf("answer=%v", result.FinalAssistant)
	}
}

func TestConfigureSessionRegistersKnowledgeSearch(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	ws := t.TempDir()
	// Force passport-like config with TokenSource so knowledgeTools installs.
	cfg := engine.Config{
		CWD: ws, PermissionMode: "interactive",
		BaseURL: "https://bridge.test/t/tenant/v1",
		TokenSource: func() (string, error) { return "tok", nil },
	}
	sctx := hostSessionContext(ws, "sess_kb_tool")
	var opts engine.Options
	app.configureSession(sctx, &cfg, &opts)
	found := false
	for _, tl := range opts.ExtraTools {
		if tl.Name() == knowledgetool.Name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ExtraTools missing knowledge_search: %v", toolNames(opts.ExtraTools))
	}
	if !strings.Contains(cfg.SystemPromptAppend, "knowledge_search") {
		t.Fatalf("missing knowledge guidance in SystemPromptAppend")
	}
}

type countingRetrieveHost struct {
	calls atomic.Int32
	hits  []knowledgetool.RawHit
}

func (h *countingRetrieveHost) Retrieve(_ context.Context, _ string, _ []string, _ knowledgetool.CallMeta) ([]knowledgetool.RawHit, string, error) {
	h.calls.Add(1)
	status := "empty"
	if len(h.hits) > 0 {
		status = "matched"
	}
	return append([]knowledgetool.RawHit(nil), h.hits...), status, nil
}

func toolNames(tools []tool.Tool) []string {
	out := make([]string, len(tools))
	for i, tl := range tools {
		out[i] = tl.Name()
	}
	return out
}

func hostSessionContext(ws, id string) host.SessionContext {
	return host.SessionContext{
		ID:       id,
		Approver: host.NewAsyncApprover(func(string, any) {}, ws),
		Emit:     func(string, any) {},
	}
}
