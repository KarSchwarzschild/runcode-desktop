package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appwire "github.com/wt68/runcode/internal/protocol"
)

// imageGenSessionApp opens a Passport-bound ordinary session for sub/tenant
// against the fixture catalog, on the fake engine build.
func imageGenSessionApp(t *testing.T, sub, tenant string) (*App, string, string) {
	t.Helper()
	isolateConfigDir(t)
	fx := loadCatalogFixture(t)
	serveCatalog(t, 200, fx.Response)
	app, _, built := newParallelApp(t)
	t.Cleanup(func() { _ = app.mgr.CloseAll(context.Background()) })
	app.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT(sub), Expiry: time.Now().Add(time.Hour)})
	id, sess := openParallelSession(t, app, built)
	markImageGenPassport(app, id, tenant)
	return app, id, sess.cwd
}

func markImageGenPassport(app *App, id, tenant string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	e := app.entryLocked(id)
	e.passport, e.tenantID = true, tenant
	app.bindKnowledgeLocked(e)
}

func setImageGen(t *testing.T, app *App, id string, enabled bool) appwire.SessionImageGenerationView {
	t.Helper()
	cur, err := app.GetSessionImageGeneration(id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: enabled, ExpectedRevision: cur.Revision})
	if err != nil {
		t.Fatalf("set %v: %v", enabled, err)
	}
	return got
}

func TestImageGenerationSessionDefaultsOffAndIsIndependent(t *testing.T) {
	app, a, _ := imageGenSessionApp(t, "user-a", "tenant-a")
	b := openSecondImageGenSession(t, app, "tenant-a")
	for _, id := range []string{a, b} {
		v, err := app.GetSessionImageGeneration(id)
		if err != nil || v.Enabled || v.Revision != 0 || v.SessionID != id {
			t.Fatalf("new session %s not off: %+v %v", id, v, err)
		}
	}
	on := setImageGen(t, app, a, true)
	if !on.Enabled || on.Revision != 1 {
		t.Fatalf("enable = %+v", on)
	}
	if v, _ := app.GetSessionImageGeneration(b); v.Enabled {
		t.Fatal("enabling A enabled B")
	}
}

func openSecondImageGenSession(t *testing.T, app *App, tenant string) string {
	t.Helper()
	cwd := t.TempDir()
	app.mu.Lock()
	app.registerSessionLocked("sess_image_second", cwd, nil, nil, nil)
	app.mu.Unlock()
	markImageGenPassport(app, "sess_image_second", tenant)
	return "sess_image_second"
}

func TestImageGenerationSessionAvailabilityFollowsSettings(t *testing.T) {
	app, id, _ := imageGenSessionApp(t, "user-a", "tenant-a")
	v := setImageGen(t, app, id, true)
	if !v.Enabled || v.Available || !strings.Contains(v.Reason, "设置") || v.Model != nil {
		t.Fatalf("unconfigured switch pretends to be ready: %+v", v)
	}
	saveImageModel(t, app, "tenant-a", "image-only-url", "")
	v, err := app.GetSessionImageGeneration(id)
	if err != nil || !v.Enabled || !v.Available || v.Model == nil || v.Model.Name != "image-only-url" || v.Size != "2048x2048" {
		t.Fatalf("configured view = %+v %v", v, err)
	}
	off := setImageGen(t, app, id, false)
	if off.Enabled || !off.Available || off.Revision != 2 {
		t.Fatalf("disable = %+v", off)
	}
}

func TestImageGenerationSessionRejectsBusyAndStaleChanges(t *testing.T) {
	app, id, _ := imageGenSessionApp(t, "user-a", "tenant-a")
	app.mu.Lock()
	app.sessions[id].turnActive = true
	app.mu.Unlock()
	if _, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: true}); err == nil {
		t.Fatal("switch changed during a running turn")
	}
	app.mu.Lock()
	app.sessions[id].turnActive = false
	app.mu.Unlock()
	if v, _ := app.GetSessionImageGeneration(id); v.Enabled || v.Revision != 0 {
		t.Fatalf("rejected change persisted: %+v", v)
	}
	on := setImageGen(t, app, id, true)
	again, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: true, ExpectedRevision: 0})
	if err != nil || again.Revision != on.Revision {
		t.Fatalf("same target not idempotent: %+v %v", again, err)
	}
	if _, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: false, ExpectedRevision: 0}); err == nil {
		t.Fatal("stale revision accepted for a different target")
	}
}

func TestImageGenerationSessionRestoresOnlyForSameIdentity(t *testing.T) {
	app, id, cwd := imageGenSessionApp(t, "user-a", "tenant-a")
	setImageGen(t, app, id, true)

	reopen := func(sub, tenant string) appwire.SessionImageGenerationView {
		t.Helper()
		next := New(&recordingSink{})
		next.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT(sub), Expiry: time.Now().Add(time.Hour)})
		next.mu.Lock()
		next.registerSessionLocked(id, cwd, nil, nil, nil)
		next.mu.Unlock()
		markImageGenPassport(next, id, tenant)
		v, err := next.GetSessionImageGeneration(id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := reopen("user-a", "tenant-a"); !v.Enabled || v.Revision != 1 {
		t.Fatalf("same identity did not restore: %+v", v)
	}
	if v := reopen("user-b", "tenant-a"); v.Enabled {
		t.Fatal("other account inherited the switch")
	}
	if v := reopen("user-a", "tenant-b"); v.Enabled {
		t.Fatal("other tenant inherited the switch")
	}
}

func TestImageGenerationSessionIneligibleAndLoggedOutSessions(t *testing.T) {
	app, id, _ := imageGenSessionApp(t, "user-a", "tenant-a")
	app.mu.Lock()
	app.sessions[id].passport = false
	app.mu.Unlock()
	v, err := app.GetSessionImageGeneration(id)
	if err != nil || v.Available || v.Reason == "" {
		t.Fatalf("custom-provider session offered image generation: %+v %v", v, err)
	}
	if _, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: true}); err == nil {
		t.Fatal("ineligible session enabled")
	}
	markImageGenPassport(app, id, "tenant-a")
	app.tokens.setInMemory(tokenSet{AccessToken: imageGenJWT("user-b"), Expiry: time.Now().Add(time.Hour)})
	if v, err := app.GetSessionImageGeneration(id); err != nil || v.Available {
		t.Fatalf("account change kept session available: %+v %v", v, err)
	}
	if _, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: true}); err == nil {
		t.Fatal("switch saved under another account")
	}
}

func TestImageGenerationSessionCorruptSidecarFailsClosed(t *testing.T) {
	app, id, cwd := imageGenSessionApp(t, "user-a", "tenant-a")
	saveImageModel(t, app, "tenant-a", "image-only-url", "")
	dir := filepath.Join(cwd, ".runcode", "image-generation", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := app.GetSessionImageGeneration(id)
	if err != nil || v.Enabled || v.Available || !strings.Contains(v.Reason, "记录") {
		t.Fatalf("corrupt sidecar not fail-closed: %+v %v", v, err)
	}
	if _, err := app.SetSessionImageGeneration(appwire.SetSessionImageGenerationRequest{SessionID: id, Enabled: true}); err == nil {
		t.Fatal("corrupt sidecar overwritten")
	}
	if err := app.SendMessage(id, "普通聊天不受影响"); err != nil {
		t.Fatalf("ordinary chat blocked by image generation storage: %v", err)
	}
}

func TestImageGenerationTurnSnapshotIsFrozenPerTurn(t *testing.T) {
	app, id, cwd := imageGenSessionApp(t, "user-a", "tenant-a")
	saveImageModel(t, app, "tenant-a", "image-only-url", "")
	setImageGen(t, app, id, true)
	e, err := app.entryOf(id)
	if err != nil {
		t.Fatal(err)
	}
	state := newImageGenerationTurnState(id, "instance-7")
	app.mu.Lock()
	e.imageGeneration = state
	app.mu.Unlock()

	app.freezeImageGenerationTurn(e)
	first := state.snapshot()
	if !first.Enabled || !first.Available || first.TurnID == "" || first.Model != "image-only-url" || first.Size != "2048x2048" ||
		first.Tenant != "tenant-a" || first.Workspace != cwd || first.Generation != "instance-7" || first.SettingsRevision != 1 || first.SwitchRevision != 1 || first.ScopeID == "" {
		t.Fatalf("frozen snapshot = %+v", first)
	}
	saveImageModel(t, app, "tenant-a", "image-only-b64", "1280x720")
	if mid := state.snapshot(); mid.Model != "image-only-url" || mid.TurnID != first.TurnID {
		t.Fatalf("settings change leaked into the running turn: %+v", mid)
	}
	app.freezeImageGenerationTurn(e)
	next := state.snapshot()
	if next.TurnID == first.TurnID || next.Model != "image-only-b64" || next.Size != "1280x720" || next.SettingsRevision != 2 {
		t.Fatalf("next independent turn = %+v", next)
	}
	setImageGen(t, app, id, false)
	app.freezeImageGenerationTurn(e)
	if off := state.snapshot(); off.Enabled || off.TurnID == next.TurnID {
		t.Fatalf("disabled turn = %+v", off)
	}
}
