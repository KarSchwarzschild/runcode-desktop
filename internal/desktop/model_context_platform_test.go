package desktop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
)

func TestModelContextPlatformDefaultsOverridesAndIsolation(t *testing.T) {
	isolateConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		budget := 64000
		if strings.Contains(r.URL.Path, "tenant-B") {
			budget = 128000
		}
		fmt.Fprintf(w, `{"data":[{"id":"same","context_tokens":%d,"max_output_tokens":4096,"max_history_messages":18},{"id":"other","context_tokens":96000,"max_output_tokens":2048,"max_history_messages":0}]}`, budget)
	}))
	defer server.Close()
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	a, b := platformModelRef("tenant-A", "same"), platformModelRef("tenant-B", "same")
	got, err := app.GetModelContextSettings(a)
	if err != nil || got.Effective.MaxContextTokens != 64000 || got.Defaults == nil {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	got, err = app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: a, Overrides: ModelContextOverride{MaxContextTokens: contextInt(0)}})
	if err != nil || got.Effective.MaxContextTokens != 0 || got.Effective.MaxOutputTokens != 4096 || got.Effective.MaxHistoryMessages != 18 || got.Defaults.MaxContextTokens != 64000 {
		t.Fatalf("single override: %+v %v", got, err)
	}
	got, err = app.GetModelContextSettings(b)
	if err != nil || got.Effective.MaxContextTokens != 128000 {
		t.Fatalf("tenant mixed: %+v %v", got, err)
	}
	got, err = app.GetModelContextSettings(platformModelRef("tenant-A", "other"))
	if err != nil || got.Effective.MaxContextTokens != 96000 {
		t.Fatalf("model mixed: %+v %v", got, err)
	}
	alien := a
	alien.Bridge = "https://different.invalid"
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: alien}); err == nil {
		t.Fatal("foreign bridge accepted")
	}
	if _, err := app.GetModelContextSettings(platformModelRef("tenant-A", "unauthorized")); err == nil {
		t.Fatal("ungranted model accepted")
	}
	got, err = app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: a})
	if err != nil || got.Effective.MaxContextTokens != 64000 || len(loadRawConfig().ModelContexts) != 0 {
		t.Fatalf("reset: %+v %v", got, err)
	}
}

func TestModelContextOldCatalogRequiresCompleteOverride(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "actor", Expiry: time.Now().Add(time.Hour)})
	ref := platformModelRef("tenant", "old")
	app.rememberModelCatalog(platformModelRef("tenant", ""), app.visionAccountIdentity(), []PassportModel{{ID: "old", ContextTokens: 32000}})
	got, err := app.GetModelContextSettings(ref)
	if err != nil || got.Defaults != nil || !strings.Contains(got.Warning, "Bridge") {
		t.Fatalf("old catalog: %+v %v", got, err)
	}
	if _, err := app.applyModelContext(context.Background(), engine.Config{}, ref); err == nil {
		t.Fatal("old catalog silently used an invented default")
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxOutputTokens: contextInt(1024), MaxContextTokens: contextInt(32000), MaxHistoryMessages: contextInt(0)}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.applyModelContext(context.Background(), engine.Config{MaxTokens: 99999, MaxContextTokens: 1, MaxHistoryMessages: 2}, ref)
	if err != nil || cfg.MaxTokens != 1024 || cfg.MaxContextTokens != 32000 || cfg.MaxHistoryMessages != 0 {
		t.Fatalf("explicit full override: %+v %v", cfg, err)
	}
}

func TestModelContextRejectsLateAccountAndReturnsPersistenceFailure(t *testing.T) {
	isolateConfigDir(t)
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"data":[{"id":"m","context_tokens":32000,"max_output_tokens":4096,"max_history_messages":0}]}`)
	}))
	defer server.Close()
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	app := New(&recordingSink{})
	app.tokens.setInMemory(tokenSet{AccessToken: "old", Expiry: time.Now().Add(time.Hour)})
	ref := platformModelRef("tenant", "m")
	done := make(chan error, 1)
	go func() { _, err := app.GetModelContextSettings(ref); done <- err }()
	<-started
	app.tokens.setInMemory(tokenSet{AccessToken: "new", Expiry: time.Now().Add(time.Hour)})
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late old-account model defaults accepted")
	}
	cacheContextModels(app, "tenant", "m")
	path, _ := desktopConfigPath()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxOutputTokens: contextInt(512)}}); err == nil {
		t.Fatal("failed atomic write reported success")
	}
}
