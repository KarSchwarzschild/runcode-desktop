package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wt68/runcode/internal/imagegentool"
	appwire "github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

// imageGenBridge is a fake Bridge: the fixture catalog (mutable) plus the
// generation route answering with the b64 fixture.
type imageGenBridge struct {
	srv     *httptest.Server
	catalog atomic.Value // []byte
	posts   atomic.Int32
	models  atomic.Value // last requested model
	fail    atomic.Int32 // non-zero: respond with this status and no body
}

func newImageGenBridge(t *testing.T) *imageGenBridge {
	t.Helper()
	b := &imageGenBridge{}
	b.catalog.Store([]byte(loadCatalogFixture(t).Response))
	var success struct {
		Bridge struct {
			Body json.RawMessage `json:"body"`
		} `json:"bridge"`
	}
	readImageGenerationFixture(t, "success-b64.json", &success)
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/models"):
			_, _ = w.Write(b.catalog.Load().([]byte))
		case strings.HasSuffix(r.URL.Path, "/v1/Images/Generations"):
			b.posts.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var req imageGenerationWireRequest
			_ = json.Unmarshal(raw, &req)
			b.models.Store(req.Model)
			if status := b.fail.Load(); status != 0 {
				w.WriteHeader(int(status))
				return
			}
			w.Header().Set("X-Request-ID", "brg-synthetic-0002")
			_, _ = w.Write(success.Bridge.Body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.srv.Close)
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", b.srv.URL)
	return b
}

func (b *imageGenBridge) dropModel(t *testing.T, id string) {
	t.Helper()
	var payload map[string][]map[string]any
	_ = json.Unmarshal(b.catalog.Load().([]byte), &payload)
	kept := payload["data"][:0]
	for _, m := range payload["data"] {
		if m["id"] != id {
			kept = append(kept, m)
		}
	}
	payload["data"] = kept
	raw, _ := json.Marshal(payload)
	b.catalog.Store(raw)
}

type imageGenFlow struct {
	app    *App
	bridge *imageGenBridge
	id     string
	cwd    string
	entry  *sessionEntry
	state  *imageGenerationTurnState
	tool   *imagegentool.Tool
}

// newImageGenFlow is a Passport session with image generation configured
// (image-only-b64 @1024x1024) and enabled, its tool wired to the host.
func newImageGenFlow(t *testing.T) *imageGenFlow {
	t.Helper()
	isolateConfigDir(t)
	bridge := newImageGenBridge(t)
	app, _, built := newParallelApp(t)
	t.Cleanup(func() { _ = app.mgr.CloseAll(context.Background()) })
	app.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT("user-a"), Expiry: time.Now().Add(time.Hour)})
	id, sess := openParallelSession(t, app, built)
	markImageGenPassport(app, id, "tenant-a")
	saveImageModel(t, app, "tenant-a", "image-only-b64", "1024x1024")
	setImageGen(t, app, id, true)
	e, err := app.entryOf(id)
	if err != nil {
		t.Fatal(err)
	}
	state := newImageGenerationTurnState(id, "instance-1")
	app.mu.Lock()
	e.imageGeneration = state
	app.mu.Unlock()
	f := &imageGenFlow{app: app, bridge: bridge, id: id, cwd: sess.cwd, entry: e, state: state, tool: app.newImageGenerationTool(state)}
	app.freezeImageGenerationTurn(e)
	return f
}

func (f *imageGenFlow) run(t *testing.T, toolUseID, input string) appwire.ImageGenerationResult {
	t.Helper()
	r, err := f.tool.Run(context.Background(), json.RawMessage(input), &tool.Context{SessionID: f.id, MessageID: "q_" + toolUseID, ToolUseID: toolUseID}, nil)
	if err != nil {
		t.Fatalf("tool returned a Go error: %v", err)
	}
	var out appwire.ImageGenerationResult
	if err := json.Unmarshal([]byte(r.Content[0].Text), &out); err != nil {
		t.Fatal(err)
	}
	if r.IsError != (out.Status != "succeeded") {
		t.Fatalf("IsError=%v for status %s", r.IsError, out.Status)
	}
	return out
}

func TestImageGenerationFlowGeneratesWithConfiguredModel(t *testing.T) {
	f := newImageGenFlow(t)
	got := f.run(t, "tu_1", `{"prompt":"校园开放日海报背景"}`)
	if got.Status != "succeeded" || len(got.Images) != 1 || got.Model != "image-only-b64" || got.Size != "1024x1024" || got.RequestID != "brg-synthetic-0002" || got.AccountingStatus != "unverified" {
		t.Fatalf("result = %+v", got)
	}
	if f.bridge.posts.Load() != 1 || f.bridge.models.Load() != "image-only-b64" {
		t.Fatalf("posts=%d model=%v", f.bridge.posts.Load(), f.bridge.models.Load())
	}
	if _, err := os.Stat(filepath.Join(f.cwd, filepath.FromSlash(got.Images[0].Path))); err != nil {
		t.Fatalf("image not in workspace: %v", err)
	}
	rec, err := readImageGenerationLedger(f.cwd, f.id, got.GenerationID)
	if err != nil || rec.Status != "succeeded" || rec.QuestionID != "q_tu_1" || rec.ToolUseID != "tu_1" || rec.TurnID != f.state.snapshot().TurnID {
		t.Fatalf("ledger = %+v %v", rec, err)
	}
}

func TestImageGenerationFlowDisabledAndIneligibleSendNothing(t *testing.T) {
	f := newImageGenFlow(t)
	setImageGen(t, f.app, f.id, false)
	f.app.freezeImageGenerationTurn(f.entry)
	if got := f.run(t, "tu_1", `{"prompt":"海报"}`); got.Code != "disabled" {
		t.Fatalf("disabled = %+v", got)
	}
	setImageGen(t, f.app, f.id, true)
	f.app.mu.Lock()
	f.entry.oaLocalModel = "local-model"
	f.app.mu.Unlock()
	f.app.freezeImageGenerationTurn(f.entry)
	if got := f.run(t, "tu_2", `{"prompt":"海报"}`); got.Status != "failed" || !strings.Contains(got.Message, "OA") {
		t.Fatalf("OA-locked session sent its content to an image model: %+v", got)
	}
	if f.bridge.posts.Load() != 0 {
		t.Fatalf("posts = %d", f.bridge.posts.Load())
	}
}

func TestImageGenerationFlowRegistersOnlyForPassportMainSessions(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	if tools := app.imageGenerationTools(&engine.Config{}, "custom-session", "instance-1"); len(tools) != 0 {
		t.Fatal("custom-provider session got image_generate")
	}
	passport := &engine.Config{TokenSource: func() (string, error) { return "t", nil }}
	tools := app.imageGenerationTools(passport, "passport-session", "instance-1")
	if len(tools) != 1 || tools[0].Name() != imagegentool.Name {
		t.Fatalf("passport tools = %v", tools)
	}
	if _, ok := tools[0].(tool.AuthorizationBindingProvider); !ok {
		t.Fatal("image_generate has no authorization binding")
	}
	app.mu.Lock()
	pending := app.pendingImageGeneration
	app.mu.Unlock()
	if pending == nil || pending.sessionID != "passport-session" {
		t.Fatal("state not parked for promotion")
	}
	if hostToolClasses[imagegentool.Name] != permissions.ClassMutating {
		t.Fatalf("image_generate class = %v, want mutating (spends money, writes files)", hostToolClasses[imagegentool.Name])
	}
}

func TestImageGenerationFlowStartSessionPromotesState(t *testing.T) {
	isolateConfigDir(t)
	newImageGenBridge(t)
	app := New(&recordingSink{})
	t.Cleanup(func() { _ = app.CloseAllSessions() })
	app.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT("user-a"), Expiry: time.Now().Add(time.Hour)})
	cacheContextModels(app, "tenant-a", "chat-legacy")
	info, err := app.StartSession(StartSessionRequest{CWD: t.TempDir(), Provider: "passport", Model: "chat-legacy", TenantID: "tenant-a", PermissionMode: "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := app.entryOf(info.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	state := e.imageGeneration
	app.mu.Unlock()
	if state == nil || state.sessionID != info.SessionID {
		t.Fatal("passport session has no image generation state")
	}
	app.onTurnStart(info.SessionID)
	first := state.snapshot().TurnID
	app.onTurnStart(info.SessionID)
	if first == "" || state.snapshot().TurnID == first {
		t.Fatal("turn start did not freeze a fresh TurnID")
	}
}

func TestImageGenerationPermissionSummaryShowsFrozenTarget(t *testing.T) {
	f := newImageGenFlow(t)
	snap := f.state.snapshot()
	summary := imageGenerationApprovalSummary(snap, json.RawMessage(`{"prompt":"校园开放日海报背景","size":"1024x1024"}`))
	for _, want := range []string{"image-only-b64", "tenant-a", "1024x1024", "校园开放日海报背景", "计费"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("approval summary %q missing %q", summary, want)
		}
	}
}
