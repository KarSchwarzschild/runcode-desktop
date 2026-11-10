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
	"sync/atomic"
	"testing"
	"time"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// Real host + actual OpenAI SSE adapter, isolated config, synthetic models only.
// The rejected script is a harmless temp-directory sentinel; it must never run.
func TestSmartReviewUsesSourceTaskModelAndScript(t *testing.T) {
	isolateConfigDir(t)
	wsA, wsB := t.TempDir(), t.TempDir()
	for ws, marker := range map[string]string{wsA: "SOURCE_A", wsB: "SOURCE_B"} {
		script := "# " + marker + "\nfrom pathlib import Path\nPath('sentinel').write_text('should not run')\n"
		if err := os.WriteFile(filepath.Join(ws, "report.py"), []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var aCalls, bCalls, judgeCalls atomic.Int32
	var rightEvidence atomic.Bool
	modelA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "fixture request", 400)
			return
		}
		aCalls.Add(1)
		raw, _ := json.Marshal(req)
		text := string(raw)
		delta := map[string]any{}
		finish := "stop"
		if strings.Contains(text, "You are an authorization gate") {
			judgeCalls.Add(1)
			rightEvidence.Store(strings.Contains(text, "SOURCE_A") && strings.Contains(text, "source-A user task") && !strings.Contains(text, "SOURCE_B") && req["model"] == "model-A")
			delta["content"] = `{"risk":"low","alignment":"unrelated","reason":"低风险但不是本次用户任务"}`
		} else {
			delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "sentinel-call", "type": "function", "function": map[string]any{"name": "Bash", "arguments": `{"command":"python report.py"}`}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []any{map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}}} {
			data, _ := json.Marshal(part)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelA.Close()
	modelB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bCalls.Add(1)
		http.Error(w, "wrong source provider", 500)
	}))
	defer modelB.Close()
	sink := &recordingSink{}
	app := New(sink)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer func() { _ = app.mgr.CloseAll(ctx) }()
	cfg := engine.Config{CWD: wsA, Model: "model-A", Provider: "openai", BaseURL: modelA.URL + "/v1", APIKey: "synthetic", PermissionMode: "judge", MaxIterations: 2}
	idA, _, err := app.mgr.Create(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CWD = wsB
	cfg.Model = "model-B"
	cfg.BaseURL = modelB.URL + "/v1"
	idB, _, err := app.mgr.Create(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	focusOn(app, idB, wsB)
	if err := app.mgr.SendMessage(idA, "source-A user task: explain this report, do not execute it"); err != nil {
		t.Fatal(err)
	}
	var approval hostproto.PermissionRequest
	waitFor(t, 10*time.Second, func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		for _, event := range sink.events {
			e, ok := event.data.(hostproto.Envelope)
			if !ok || e.SessionID != idA {
				continue
			}
			if p, ok := e.Payload.(hostproto.PermissionRequest); ok {
				approval = p
				return true
			}
		}
		return false
	}, "source action did not require manual approval")
	if judgeCalls.Load() != 1 || !rightEvidence.Load() || bCalls.Load() != 0 {
		t.Fatalf("source review mismatch: A=%d judge=%d correct=%v B=%d", aCalls.Load(), judgeCalls.Load(), rightEvidence.Load(), bCalls.Load())
	}
	if approval.HarmReason == "" {
		t.Fatal("missing task-related manual approval explanation")
	}
	if err := app.mgr.ResolvePermission(idA, approval.ID, "deny"); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 1, time.Now().Add(10*time.Second))
	if _, err := os.Stat(filepath.Join(wsA, "sentinel")); !os.IsNotExist(err) {
		t.Fatal("unrelated action executed before consent")
	}
	// Keep a judge from the old generation, then recreate the same logical id.
	var generation string
	if err := app.mgr.WithSessionOperation(ctx, idA, func(_ context.Context, a host.SessionAccess) error { generation = a.Generation; return nil }); err != nil {
		t.Fatal(err)
	}
	old := modelHarmJudge{app: app, sessionID: idA, generation: generation}
	if err := app.mgr.Close(ctx, idA); err != nil {
		t.Fatal(err)
	}
	cfg.CWD = wsA
	cfg.Model = "model-A"
	cfg.BaseURL = modelA.URL + "/v1"
	cfg.SessionID = idA
	if _, _, err := app.mgr.Create(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	taskCtx := permissions.WithTaskEvidence(ctx, permissions.TaskEvidence{SessionID: idA, Complete: true, Questions: []permissions.UserTask{{Text: "source-A user task"}}})
	if _, err := old.Assess(taskCtx, permissions.Action{InputComplete: true}); err == nil {
		t.Fatal("old generation review borrowed replacement session")
	}
	if bCalls.Load() != 0 || judgeCalls.Load() != 1 {
		t.Fatal("stale judge called another model")
	}
}

func TestReviewCredentialGateRejectsChangedAccount(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "synthetic-one", Expiry: time.Now().Add(time.Hour)})
	epoch := app.tokens.loginEpoch()
	cfg := engine.Config{CWD: t.TempDir(), TokenSource: app.tokens.Token, PermissionMode: "judge"}
	opts := engine.Options{}
	app.configureSession(host.SessionContext{ID: "source", Generation: "generation", Emit: func(string, any) {}}, &cfg, &opts)
	if value, err := cfg.TokenSource(); err != nil || value != "synthetic-one" {
		t.Fatal("initial credential source rejected", err)
	}
	// Token refresh changes credentials but not the login epoch.
	app.tokens.mu.Lock()
	app.tokens.ts.AccessToken = "synthetic-refresh"
	app.tokens.mu.Unlock()
	if value, err := cfg.TokenSource(); err != nil || value != "synthetic-refresh" {
		t.Fatal("ordinary refresh changed source ownership", err)
	}
	app.tokens.setInMemory(tokenSet{AccessToken: "synthetic-other-account", Expiry: time.Now().Add(time.Hour)})
	if value, err := cfg.TokenSource(); err == nil || value != "" {
		t.Fatal("old session obtained another account's credential")
	}
	judge := modelHarmJudge{app: app, sessionID: "source", generation: "generation", passport: true, account: epoch}
	ctx := permissions.WithTaskEvidence(context.Background(), permissions.TaskEvidence{SessionID: "source", Complete: true, Questions: []permissions.UserTask{{Text: "write report"}}})
	if _, err := judge.Assess(ctx, permissions.Action{InputComplete: true}); err == nil || !strings.Contains(err.Error(), "账号") {
		t.Fatal("account mismatch did not stop review before provider access", err)
	}
}
