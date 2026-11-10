package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/sessions"
)

// Actual App/host/provider traffic with isolated configuration, no real account
// or endpoint. A simultaneous blocked turn catches accidental focused closure.
func TestModelContextActualSwitchResumeForkAndBackgroundTurn(t *testing.T) {
	isolateConfigDir(t)
	type request struct {
		Model     string            `json:"model"`
		MaxTokens int               `json:"max_tokens"`
		Tools     []json.RawMessage `json:"tools"`
		Messages  []json.RawMessage `json:"messages"`
	}
	var mu sync.Mutex
	var requests []request
	blocked, release := make(chan struct{}), make(chan struct{})
	var canceled atomic.Bool
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"A","context_tokens":120000,"max_output_tokens":1024,"max_history_messages":0},{"id":"B","context_tokens":64000,"max_output_tokens":512,"max_history_messages":0}]}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if len(req.Tools) > 0 {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
			if strings.Contains(string(mustJSON(t, req.Messages)), "block-other") {
				close(blocked)
				select {
				case <-release:
				case <-r.Context().Done():
					canceled.Store(true)
					return
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", `{"id":"fixture","choices":[{"index":0,"delta":{"content":"fixture answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":400,"completion_tokens":5}}`)
	}))
	defer server.Close()
	// Unblock before httptest shutdown even when an assertion fails.
	defer releaseOnce.Do(func() { close(release) })
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	sink := &recordingSink{}
	app := New(sink)
	defer app.CloseSession("")
	app.tokens.setInMemory(tokenSet{AccessToken: "fixture-actor", Expiry: time.Now().Add(time.Hour)})
	ws := t.TempDir()
	info, err := app.StartSession(StartSessionRequest{CWD: ws, Provider: "passport", Model: "A", TenantID: "tenant", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	original := info.SessionID
	defer app.CloseSession(original)
	first, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: original, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 1, time.Now().Add(10*time.Second))
	if info.MaxContextTokens != 120000 {
		t.Fatalf("A budget=%d", info.MaxContextTokens)
	}
	other, err := app.OpenSession(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseSession(other.SessionID)
	if err := app.SendMessage(other.SessionID, "block-other"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("other turn did not start")
	}
	if _, err := app.SwitchModel(other.SessionID, "platform", "B"); err == nil {
		t.Fatal("running session switched")
	}
	info, err = app.SwitchModel(original, "platform", "B")
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "B" || info.MaxContextTokens != 64000 || info.SessionID != original || app.focusedSessionID() != other.SessionID || canceled.Load() {
		t.Fatalf("wrong target/focus/budget: %+v canceled=%v", info, canceled.Load())
	}
	second, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: original, Text: "second"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 2, time.Now().Add(10*time.Second))
	// Persistence while another turn runs cannot mutate or close either engine.
	ref := platformModelRef("tenant", "B")
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxOutputTokens: contextInt(333), MaxContextTokens: contextInt(96000)}}); err != nil {
		t.Fatal(err)
	}
	current, err := app.Status(original)
	if err != nil || current.MaxContextTokens != 64000 || canceled.Load() {
		t.Fatalf("save changed live state: %+v %v", current, err)
	}
	info, err = app.SwitchModel(original, "platform", "B")
	if err != nil || info.MaxContextTokens != 96000 || info.Model != "B" {
		t.Fatalf("reselect override: %+v %v", info, err)
	}
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: original, Text: "third"}); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 3, time.Now().Add(10*time.Second))
	// Resume must use persisted model B even though the focused next config is A.
	if err := app.CloseSession(original); err != nil {
		t.Fatal(err)
	}
	// The history picker resumes within its displayed workspace.
	placeholder, err := app.OpenSession(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseSession(placeholder.SessionID)
	resumed, err := app.ResumeSession(original)
	if err != nil || resumed.Info.Model != "B" || resumed.Info.MaxContextTokens != 96000 {
		t.Fatalf("resume chose wrong model/budget: %+v %v", resumed.Info, err)
	}
	if _, err := app.GetQuestion(QuestionReference{SessionID: original, QuestionID: first.QuestionID}); err != nil {
		t.Fatalf("lost history after rebuild: %v", err)
	}
	fork, err := app.ForkQuestion(QuestionReference{SessionID: original, QuestionID: second.QuestionID})
	if err != nil || fork.Info.Model != "B" || fork.Info.MaxContextTokens != 96000 || app.focusedSessionID() != original {
		t.Fatalf("fork budget/focus: %+v %v", fork.Info, err)
	}
	defer app.CloseSession(fork.Info.SessionID)
	if _, err := app.SubmitQuestion(SubmitQuestionRequest{SessionID: fork.Info.SessionID, Text: "retry second"}); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 4, time.Now().Add(10*time.Second))
	mu.Lock()
	actual := append([]request(nil), requests...)
	mu.Unlock()
	// Exclude the intentionally blocked A request. The rest are real main requests.
	var pairs []string
	for _, req := range actual {
		if !strings.Contains(string(mustJSON(t, req.Messages)), "block-other") {
			pairs = append(pairs, fmt.Sprintf("%s/%d", req.Model, req.MaxTokens))
		}
	}
	if strings.Join(pairs, ",") != "A/1024,B/512,B/333,B/333" {
		t.Fatalf("actual main requests=%v", pairs)
	}
	if canceled.Load() {
		t.Fatal("background turn canceled by target rebuild")
	}
	releaseOnce.Do(func() { close(release) })
	waitForTurns(t, sink, 5, time.Now().Add(10*time.Second))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestModelContextRestoreMetadataAndOALockPrecedence(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	cacheContextModels(app, "tenant", "A", "B")
	ws := t.TempDir()
	cfg, err := buildConfig(StartSessionRequest{CWD: ws, Provider: "passport", Model: "A"})
	if err != nil {
		t.Fatal(err)
	}
	cfg = app.applyPassport(cfg, StartSessionRequest{Provider: "passport", TenantID: "tenant"})
	app.startMu.Lock()
	info, err := app.buildSessionHeld(cfg, true, "tenant")
	app.startMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseSession(info.SessionID)
	if err := app.mgr.WithIdleSession(context.Background(), info.SessionID, func(ctx context.Context, _ host.Session, backend sessions.Backend) error {
		return backend.SaveMeta(ctx, info.SessionID, sessions.SessionMeta{Model: "B", PlanMode: true})
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseSession(info.SessionID); err != nil {
		t.Fatal(err)
	}
	// Close persists runtime model A, so prepare the fixture's saved B afterwards.
	backend, err := sessions.OpenBackend(ws, sessions.BackendJSONL)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close(context.Background())
	if err := backend.SaveMeta(context.Background(), info.SessionID, sessions.SessionMeta{Model: "B", PlanMode: true}); err != nil {
		t.Fatal(err)
	}
	cfg.Resume = info.SessionID
	restored, err := restoredContextModel(context.Background(), cfg, true)
	if err != nil || restored.Model != "B" {
		t.Fatalf("stored model: %s %v", restored.Model, err)
	}
	if err := writeOALock(ws, info.SessionID, "A"); err != nil {
		t.Fatal(err)
	}
	restored = app.restoreOALockHeld(restored)
	if restored.Model != "A" {
		t.Fatal("OA lock did not take precedence")
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: platformModelRef("tenant", "A"), Overrides: ModelContextOverride{MaxContextTokens: contextInt(0)}}); err != nil {
		t.Fatal(err)
	}
	effective, err := app.applyModelContext(context.Background(), restored, platformModelRef("tenant", restored.Model))
	if err != nil || effective.MaxContextTokens != 0 {
		t.Fatalf("OA override lost: %d %v", effective.MaxContextTokens, err)
	}
	meta, err := backend.LoadMeta(context.Background(), info.SessionID)
	if err != nil || meta.Model != "B" || !meta.PlanMode {
		t.Fatalf("saved history metadata changed: %+v %v", meta, err)
	}
}

func TestModelContextSwitchToSmallModelCompactsBeforeMainRequest(t *testing.T) {
	isolateConfigDir(t)
	const evidence = "critical-budget-evidence"
	thinking := strings.Repeat("token payload for budget migration ", 5000) + evidence
	var summaries, smallCalls atomic.Int32
	var summarizedThinking atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"large","context_tokens":200000,"max_output_tokens":81920,"max_history_messages":0},{"id":"small","context_tokens":32768,"max_output_tokens":4096,"max_history_messages":0}]}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model     string            `json:"model"`
			MaxTokens int               `json:"max_tokens"`
			Tools     []json.RawMessage `json:"tools"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var system string
		if len(req.Messages) > 0 {
			_ = json.Unmarshal(req.Messages[0].Content, &system)
		}
		delta := map[string]any{"content": "fixture answer"}
		switch {
		case strings.HasPrefix(system, "You are condensing"):
			summaries.Add(1)
			if strings.Contains(string(mustJSON(t, req.Messages)), evidence) {
				summarizedThinking.Store(true)
			}
			delta["content"] = "Earlier work and reasoning preserved: " + evidence
		case len(req.Tools) > 0 && req.Model == "large":
			delta["reasoning_content"] = thinking
		case len(req.Tools) > 0 && req.Model == "small":
			smallCalls.Add(1)
			if summaries.Load() == 0 {
				t.Error("small main request sent before compaction")
			}
			if req.MaxTokens > 4096 {
				t.Errorf("small output retained large budget: %d", req.MaxTokens)
			}
			if strings.Count(string(mustJSON(t, req.Messages)), "token payload for budget migration") > 100 {
				t.Error("small main request retained oversized raw history")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		data := map[string]any{"id": "fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 8}}
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", mustJSON(t, data))
	}))
	defer server.Close()
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	sink := &recordingSink{}
	app := New(sink)
	app.tokens.setInMemory(tokenSet{AccessToken: "fixture-actor", Expiry: time.Now().Add(time.Hour)})
	info, err := app.StartSession(StartSessionRequest{CWD: t.TempDir(), Provider: "passport", Model: "large", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseSession(info.SessionID)
	if err := app.SendMessage(info.SessionID, "first"); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 1, time.Now().Add(10*time.Second))
	info, err = app.SwitchModel(info.SessionID, "platform", "small")
	if err != nil || info.MaxContextTokens != 32768 {
		t.Fatalf("small budget: %+v %v", info, err)
	}
	if err := app.SendMessage(info.SessionID, "continue after changing the model"); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 2, time.Now().Add(15*time.Second))
	if summaries.Load() == 0 || smallCalls.Load() != 1 || !summarizedThinking.Load() {
		t.Fatalf("summaries=%d main=%d thinking-preserved=%v", summaries.Load(), smallCalls.Load(), summarizedThinking.Load())
	}
}
