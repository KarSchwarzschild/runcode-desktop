package desktop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appwire "github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
)

func unreachableBridge(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bridge down", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
}

func expireModelCatalog(app *App, scope modelReference, age time.Duration) {
	app.mu.Lock()
	defer app.mu.Unlock()
	key := modelCatalogKey(scope, app.visionAccountIdentity())
	entry := app.modelCatalogs[key]
	entry.at = time.Now().Add(-age)
	app.modelCatalogs[key] = entry
}

// A Bridge outage must not stop a conversation from opening: builds reuse the
// account's last snapshot, while the settings page still reports the failure.
func TestModelContextOutageUsesStaleSnapshotForBuilds(t *testing.T) {
	isolateConfigDir(t)
	unreachableBridge(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	scope, ref := platformModelRef("tenant", ""), platformModelRef("tenant", "m")
	app.rememberModelCatalog(scope, app.visionAccountIdentity(), []PassportModel{{ID: "m", ContextTokens: 64000, MaxOutputTokens: contextInt(4096), MaxHistoryMessages: contextInt(12)}})
	expireModelCatalog(app, scope, 2*modelCatalogTTL)

	cfg, err := app.applyModelContext(context.Background(), engine.Config{}, ref)
	if err != nil || cfg.MaxContextTokens != 64000 || cfg.MaxTokens != 4096 || cfg.MaxHistoryMessages != 12 {
		t.Fatalf("stale snapshot not used: %+v %v", cfg, err)
	}
	if _, err := app.GetModelContextSettings(ref); err == nil {
		t.Fatal("settings view silently served an expired catalog")
	}
	// The snapshot still answers authorization: an id it lacks stays rejected.
	if _, err := app.applyModelContext(context.Background(), engine.Config{}, platformModelRef("tenant", "gpt-4o")); err == nil || !strings.Contains(err.Error(), "无权") {
		t.Fatalf("unknown model accepted from snapshot: %v", err)
	}
	expireModelCatalog(app, scope, modelCatalogStaleLimit+time.Minute)
	if _, ok := app.staleModelCatalog(scope, app.visionAccountIdentity()); ok {
		t.Fatal("snapshot older than the stale limit was trusted")
	}
}

func TestModelContextOutageWithoutSnapshotUsesLocalOverrides(t *testing.T) {
	isolateConfigDir(t)
	unreachableBridge(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	ref := platformModelRef("tenant", "m")
	if err := updateRawConfig(func(cfg *desktopConfig) error {
		cfg.ModelContexts = append(cfg.ModelContexts, appwire.ModelContextPreference{Model: ref, Overrides: ModelContextOverride{MaxContextTokens: contextInt(32000)}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A partial override would leave the unset budgets at 0 for the session's
	// whole lifetime, silently switching compaction off.
	if _, err := app.applyModelContext(context.Background(), engine.Config{}, ref); !errors.Is(err, errModelCatalogUnavailable) {
		t.Fatalf("partial offline overrides accepted: %v", err)
	}
	if err := updateRawConfig(func(cfg *desktopConfig) error {
		cfg.ModelContexts = []appwire.ModelContextPreference{{Model: ref, Overrides: ModelContextOverride{
			MaxContextTokens: contextInt(32000), MaxOutputTokens: contextInt(2048), MaxHistoryMessages: contextInt(0),
		}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.applyModelContext(context.Background(), engine.Config{MaxTokens: 99999, MaxHistoryMessages: 7}, ref)
	if err != nil || cfg.MaxContextTokens != 32000 || cfg.MaxTokens != 2048 || cfg.MaxHistoryMessages != 0 {
		t.Fatalf("offline build: %+v %v", cfg, err)
	}
}

// The sidecar records no connection, so a model id from a custom connection
// must not turn a passport resume into an authorization failure.
func TestKeepRestoredPlatformModel(t *testing.T) {
	isolateConfigDir(t)
	unreachableBridge(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	if !app.keepRestoredPlatformModel(context.Background(), "tenant", "anything") {
		t.Fatal("no catalog to judge by, yet the recorded model was dropped")
	}
	scope := platformModelRef("tenant", "")
	app.rememberModelCatalog(scope, app.visionAccountIdentity(), []PassportModel{{ID: "m"}})
	expireModelCatalog(app, scope, 2*modelCatalogTTL)
	if !app.keepRestoredPlatformModel(context.Background(), "tenant", "m") {
		t.Fatal("platform model dropped")
	}
	if app.keepRestoredPlatformModel(context.Background(), "tenant", "gpt-4o") {
		t.Fatal("custom connection model kept on a passport resume")
	}
}

func TestCustomProfileForModel(t *testing.T) {
	stored := desktopConfig{CustomModels: []CustomModel{
		{Name: "work", Model: "gpt-4o"},
		{Name: "claude-x", Model: "other"},
		{Name: "a", Model: "dup"},
		{Name: "b", Model: "dup"},
	}}
	for _, tc := range []struct {
		model, want string
		fails       bool
	}{
		{model: "current-model", want: "current"},
		{model: "gpt-4o", want: "work"},
		// A model id equal to some profile's *name* must not select that profile.
		{model: "claude-x", fails: true},
		{model: "dup", fails: true},
	} {
		got, err := customProfileForModel(stored, "current", "current-model", tc.model)
		if tc.fails != (err != nil) || got != tc.want {
			t.Errorf("%q: got %q %v", tc.model, got, err)
		}
	}
}

// The old session must close before its replacement builds (one live session
// per resume id), so a failed build has to reopen the original connection
// instead of leaving the conversation closed.
func TestRebuildFailureReopensOriginalSession(t *testing.T) {
	isolateConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { visionSSE(w, "ok") }))
	defer server.Close()
	app := New(&recordingSink{})
	defer func() {
		for _, id := range app.mgr.List() {
			_ = app.CloseSession(id)
		}
	}()
	if _, err := app.SaveCustomModel(SaveCustomModelRequest{Name: "first", Provider: "openai", Model: "first", BaseURL: server.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	info, err := app.StartSession(StartSessionRequest{CWD: t.TempDir(), Provider: "openai", CustomModelName: "first", Model: "first", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := app.entryOf(info.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	broken := e.connectionConfig
	app.mu.Unlock()
	broken.Provider, broken.Model = "no-such-provider", "second"

	app.startMu.Lock()
	_, err = app.rebuildResumingWithConnectionHeld(broken, info.SessionID, false, "", modelReference{})
	app.startMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "已恢复原会话") {
		t.Fatalf("rebuild error = %v, want a restored-session failure", err)
	}
	restored, err := app.entryOf(info.SessionID)
	if err != nil || restored.closed || restored.modelRef.Name != "first" {
		t.Fatalf("original session not reopened: %+v %v", restored, err)
	}
	if st, err := app.Status(info.SessionID); err != nil || st.Model != "first" {
		t.Fatalf("restored status: %+v %v", st, err)
	}
}

// Deduping the catalog by ID must not hide a Bridge that marks the same model
// as vision default twice with conflicting capabilities.
func TestVisionTargetReportsDuplicateDefaultsBeforeDedupe(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	actor := app.visionAccountIdentity()
	source := platformModelRef("tenant", "chat")
	app.rememberModelCatalog(platformModelRef("tenant", ""), actor, []PassportModel{
		{ID: "chat"},
		{ID: "eye", VisionDefault: true, SupportsImages: visionBool(false)},
		{ID: "eye", VisionDefault: true, SupportsImages: visionBool(true)},
	})
	if _, err := app.visionTarget(context.Background(), "", "", actor, source, desktopConfig{}); err == nil || !strings.Contains(err.Error(), "多个默认识图模型") {
		t.Fatalf("duplicate vision defaults accepted: %v", err)
	}
	app.rememberModelCatalog(platformModelRef("tenant", ""), actor, []PassportModel{
		{ID: "chat"},
		{ID: "eye", VisionDefault: true, SupportsImages: visionBool(true)},
	})
	got, err := app.visionTarget(context.Background(), "", "", actor, source, desktopConfig{})
	if err != nil || got != platformModelRef("tenant", "eye") {
		t.Fatalf("single vision default: %+v %v", got, err)
	}
}

// Once a fetch has failed, builds within the retry window must answer from the
// fallback without paying another Bridge timeout; settings still retry at once.
func TestModelCatalogFailureSkipsRefetchForBuilds(t *testing.T) {
	isolateConfigDir(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "bridge down", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	scope, ref := platformModelRef("tenant", ""), platformModelRef("tenant", "m")
	app.rememberModelCatalog(scope, app.visionAccountIdentity(), []PassportModel{{ID: "m", ContextTokens: 64000, MaxOutputTokens: contextInt(4096), MaxHistoryMessages: contextInt(12)}})
	expireModelCatalog(app, scope, 2*modelCatalogTTL)

	build := func() {
		t.Helper()
		if cfg, err := app.applyModelContext(context.Background(), engine.Config{}, ref); err != nil || cfg.MaxContextTokens != 64000 {
			t.Fatalf("stale build: %+v %v", cfg, err)
		}
	}
	build()
	first := hits.Load()
	if first == 0 {
		t.Fatal("first build never asked the Bridge")
	}
	build()
	build()
	if !app.keepRestoredPlatformModel(context.Background(), "tenant", "m") {
		t.Fatal("platform model dropped during outage")
	}
	if got := hits.Load(); got != first {
		t.Fatalf("builds inside the retry window hit the Bridge again: %d requests, want %d", got, first)
	}
	if _, err := app.GetModelContextSettings(ref); err == nil {
		t.Fatal("settings served an expired catalog")
	}
	if hits.Load() == first {
		t.Fatal("settings did not retry the Bridge")
	}
	// Past the window, builds probe the Bridge again.
	app.mu.Lock()
	key := modelCatalogKey(scope, app.visionAccountIdentity())
	failure := app.modelCatalogFailures[key]
	failure.at = time.Now().Add(-modelCatalogRetryAfter)
	app.modelCatalogFailures[key] = failure
	app.mu.Unlock()
	before := hits.Load()
	build()
	if hits.Load() == before {
		t.Fatal("build after the retry window did not probe the Bridge")
	}
}
