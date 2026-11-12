package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wt68/runcode/internal/imagegentool"
	appwire "github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

// countingImageHost 包一层 Host：审批拒绝／取消时 Generate 不得被调用。
type countingImageHost struct {
	inner imagegentool.Host
	n     *atomic.Int32
}

func (h countingImageHost) Generate(ctx context.Context, in imagegentool.Input, meta imagegentool.CallMeta) (appwire.ImageGenerationResult, error) {
	h.n.Add(1)
	return h.inner.Generate(ctx, in, meta)
}

type imageGenApprovalSession struct {
	app       *App
	bridge    *imageGenBridge
	cwd       string
	sessionID string
	session   *engine.Session
	approver  *host.AsyncApprover
	hostCalls *atomic.Int32
	approvals *atomic.Int32
}

// startImageGenApprovalSession 走真实 configureSession → 引擎回合 → AsyncApprover 链。
// 假 LLM 只发起一次 image_generate；Bridge 生图路由计数 POST。
func startImageGenApprovalSession(t *testing.T, onPermission func(*host.AsyncApprover, PermissionRequest)) *imageGenApprovalSession {
	t.Helper()
	isolateConfigDir(t)
	bridge := newImageGenBridge(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT("user-a"), Expiry: time.Now().Add(time.Hour)})
	saveImageModel(t, app, "tenant-a", "image-only-b64", "1024x1024")

	cwd := t.TempDir()
	sessionID := "image-gen-approval"
	input, _ := json.Marshal(map[string]string{"prompt": "校园开放日海报背景", "size": "1024x1024"})
	var requests atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls := requests.Add(1)
		delta := map[string]any{"content": "已按审批结果处理。"}
		finish := "stop"
		if calls == 1 {
			delta = map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_image_gen", "type": "function",
				"function": map[string]any{"name": imagegentool.Name, "arguments": string(input)},
			}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []any{
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}},
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}},
		} {
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)

	cfg := engine.Config{
		CWD: cwd, SessionID: sessionID, Provider: "openai", Model: "image-gen-approval",
		BaseURL: model.URL + "/v1", APIKey: "fixture", PermissionMode: "interactive",
		MaxIterations: 3, TokenSource: app.tokens.Token,
	}
	hostCalls, approvals := &atomic.Int32{}, &atomic.Int32{}
	var approver *host.AsyncApprover
	approver = host.NewAsyncApprover(func(event string, payload any) {
		if event != EventPermissionRequest {
			return
		}
		req := payload.(PermissionRequest)
		if req.Summary.ToolName != imagegentool.Name {
			t.Errorf("unexpected approval tool: %+v", req.Summary)
		}
		approvals.Add(1)
		onPermission(approver, req)
	}, cwd)
	t.Cleanup(approver.DenyAll)

	opts := engine.Options{}
	app.configureSession(host.SessionContext{
		ID: sessionID, Generation: "gen-approval", Approver: approver, Emit: func(string, any) {},
	}, &cfg, &opts)

	found := false
	for _, extra := range opts.ExtraTools {
		ig, ok := extra.(*imagegentool.Tool)
		if !ok {
			continue
		}
		found = true
		ig.Host = countingImageHost{inner: ig.Host, n: hostCalls}
	}
	if !found {
		t.Fatal("configureSession 未注册 image_generate")
	}

	app.mu.Lock()
	state := app.pendingImageGeneration
	app.mu.Unlock()
	if state == nil {
		t.Fatal("image generation state not parked")
	}
	account, epoch, err := app.imageGenerationAccount()
	if err != nil {
		t.Fatal(err)
	}
	bridgeURL := imageGenerationBridge()
	state.setSnapshot(imageGenerationSnapshot{
		Enabled: true, Available: true,
		SessionID: sessionID, TurnID: "turn_approval_1", Generation: "gen-approval", Workspace: cwd,
		Account: account, Epoch: epoch, Bridge: bridgeURL, Tenant: "tenant-a",
		ScopeID: imageGenerationScope(account, bridgeURL, "tenant-a"),
		Model:   "image-only-b64", Size: "1024x1024", SupportedSizes: []string{"1024x1024"},
		ResponseFormat: "b64_json",
	})
	app.mu.Lock()
	app.pendingImageGeneration = state
	app.mu.Unlock()

	session, err := engine.Build(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	return &imageGenApprovalSession{
		app: app, bridge: bridge, cwd: cwd, sessionID: sessionID, session: session,
		approver: approver, hostCalls: hostCalls, approvals: approvals,
	}
}

func assertNoImageGenerationSubmit(t *testing.T, f *imageGenApprovalSession) {
	t.Helper()
	if f.hostCalls.Load() != 0 {
		t.Fatalf("Host.Generate called %d times after approval rejection/cancel", f.hostCalls.Load())
	}
	if f.bridge.posts.Load() != 0 {
		t.Fatalf("Bridge POST = %d, want 0", f.bridge.posts.Load())
	}
	recs, err := listImageGenerationLedgers(f.cwd, f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("ledger records = %+v, want none (no submitted)", recs)
	}
}

func TestImageGenerationApprovalDeniedDoesNotSubmit(t *testing.T) {
	f := startImageGenApprovalSession(t, func(approver *host.AsyncApprover, req PermissionRequest) {
		if err := approver.Resolve(req.ID, "deny"); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.session.RunTurn(context.Background(), "请生成一张校园开放日海报"); err != nil {
		t.Fatal(err)
	}
	if f.approvals.Load() != 1 {
		t.Fatalf("approvals = %d, want 1", f.approvals.Load())
	}
	assertNoImageGenerationSubmit(t, f)
}

func TestImageGenerationApprovalCancelledDoesNotSubmit(t *testing.T) {
	// 审批通过前取消回合：Host／POST／账本都必须为零。
	// 已经发出的取消不能假装免费 failed（见 TestImageGenerationCancelAfterSendIsNotFreeFailure
	// 与 client fixture cancel）。
	// emit 回调必须立刻返回，让 AsyncApprover.Prompt 在 select 里等 ctx；
	// 若回调自己阻塞，取消回合解不开。
	ready := make(chan struct{}, 1)
	f := startImageGenApprovalSession(t, func(_ *host.AsyncApprover, _ PermissionRequest) {
		select {
		case ready <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := f.session.RunTurn(ctx, "请生成一张校园开放日海报")
		errCh <- err
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for approval prompt")
	}
	// 给 Prompt 进入 select 一点时间，再取消回合。
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled turn")
	}
	if f.approvals.Load() != 1 {
		t.Fatalf("approvals = %d, want 1", f.approvals.Load())
	}
	assertNoImageGenerationSubmit(t, f)
}

func TestImageGenerationCancelAfterSendIsNotFreeFailure(t *testing.T) {
	// 请求已发出后的取消：必须是 cancelled（可能已生成／计费），不能写成免费 failed。
	f := newImageGenFlow(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	f.bridge.srv.Close()
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/models"):
			_, _ = w.Write(f.bridge.catalog.Load().([]byte))
		case strings.HasSuffix(r.URL.Path, "/v1/Images/Generations"):
			posts.Add(1)
			close(entered)
			<-release
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"e30="}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", srv.URL)

	snap := f.state.snapshot()
	snap.Bridge = srv.URL
	f.state.setSnapshot(snap)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan appwire.ImageGenerationResult, 1)
	go func() {
		r, err := f.tool.Run(ctx, json.RawMessage(`{"prompt":"海报"}`), &tool.Context{
			SessionID: f.id, MessageID: "q_cancel", ToolUseID: "tu_cancel",
		}, nil)
		if err != nil {
			t.Errorf("tool Go error: %v", err)
		}
		var out appwire.ImageGenerationResult
		_ = json.Unmarshal([]byte(r.Content[0].Text), &out)
		done <- out
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("POST never started")
	}
	cancel()
	var got appwire.ImageGenerationResult
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("tool did not finish after cancel")
	}
	close(release)
	if posts.Load() != 1 {
		t.Fatalf("posts = %d", posts.Load())
	}
	if got.Status != "cancelled" {
		t.Fatalf("status = %s, want cancelled (not free failed)", got.Status)
	}
	if strings.Contains(got.Message, "免费") || !strings.Contains(got.Message, "可能已生成") {
		t.Fatalf("cancel after send must warn about possible charge, got %q", got.Message)
	}
}
