package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
)

func cacheContextModels(app *App, tenant string, ids ...string) {
	models := make([]PassportModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, PassportModel{ID: id, ContextTokens: 32000, MaxOutputTokens: contextInt(4096), MaxHistoryMessages: contextInt(0)})
	}
	app.rememberModelCatalog(platformModelRef(tenant, ""), app.visionAccountIdentity(), models)
}

func contextInt(v int) *int { return &v }

func saveContextFixture(t *testing.T, app *App, name, model, baseURL string, values ModelContextValues) {
	t.Helper()
	if _, err := app.SaveCustomModel(SaveCustomModelRequest{Name: name, Provider: "openai", Model: model, BaseURL: baseURL}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{
		Model:     ModelReference{Kind: "custom", Name: name},
		Overrides: ModelContextOverride{MaxOutputTokens: &values.MaxOutputTokens, MaxContextTokens: &values.MaxContextTokens, MaxHistoryMessages: &values.MaxHistoryMessages},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestModelContextMergeAndValidation(t *testing.T) {
	defaults := &ModelContextValues{MaxOutputTokens: 4096, MaxContextTokens: 32000, MaxHistoryMessages: 80}
	got := mergeModelContext(defaults, ModelContextOverride{MaxContextTokens: contextInt(0)})
	if got != (ModelContextValues{MaxOutputTokens: 4096, MaxHistoryMessages: 80}) {
		t.Fatalf("zero was lost: %+v", got)
	}
	if *defaults != (ModelContextValues{MaxOutputTokens: 4096, MaxContextTokens: 32000, MaxHistoryMessages: 80}) {
		t.Fatal("defaults mutated")
	}
	for _, invalid := range []ModelContextOverride{
		{MaxOutputTokens: contextInt(0)}, {MaxOutputTokens: contextInt(-1)},
		{MaxContextTokens: contextInt(-1)}, {MaxHistoryMessages: contextInt(-1)},
	} {
		if validateModelContextOverride(invalid) == nil {
			t.Fatalf("invalid override accepted: %+v", invalid)
		}
	}
	data, err := json.Marshal(ModelContextOverride{MaxContextTokens: contextInt(0)})
	if err != nil || string(data) != `{"maxContextTokens":0}` {
		t.Fatalf("zero JSON: %s %v", data, err)
	}
}

func TestModelContextCustomPersistenceRenameDeleteAndReset(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	saveContextFixture(t, app, "profile", "shared", "https://example.invalid/v1", ModelContextValues{MaxOutputTokens: 1024, MaxContextTokens: 8000})
	ref := ModelReference{Kind: "custom", Name: "profile"}
	got, err := app.GetModelContextSettings(ref)
	if err != nil || got.Defaults != nil || got.Effective.MaxContextTokens != 8000 {
		t.Fatalf("custom view: %+v %v", got, err)
	}
	if _, err := app.SaveCustomModel(SaveCustomModelRequest{OriginalName: "profile", Name: "renamed", Provider: "openai", Model: "shared", BaseURL: "https://example.invalid/v1"}); err != nil {
		t.Fatal(err)
	}
	ref.Name = "renamed"
	got, err = app.GetModelContextSettings(ref)
	if err != nil || got.Effective.MaxContextTokens != 8000 {
		t.Fatalf("rename lost override: %+v %v", got, err)
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref}); err != nil {
		t.Fatal(err)
	}
	if len(loadRawConfig().ModelContexts) != 0 {
		t.Fatal("reset did not remove override")
	}
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxContextTokens: contextInt(0)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DeleteCustomModel("renamed"); err != nil {
		t.Fatal(err)
	}
	if len(loadRawConfig().ModelContexts) != 0 {
		t.Fatal("delete leaked override")
	}
}

func TestModelContextLegacyGlobalSettingsHaveNoEffect(t *testing.T) {
	isolateConfigDir(t)
	path, _ := desktopConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"maxTokens":999999,"maxContextTokens":1,"maxHistoryMessages":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var req StartSessionRequest
	if err := json.Unmarshal([]byte(`{"maxTokens":999999,"maxContextTokens":1,"maxHistoryMessages":1}`), &req); err != nil {
		t.Fatal(err)
	}
	req.CWD, req.Model = t.TempDir(), "unnamed"
	cfg, err := buildConfig(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(&recordingSink{}).applyModelContext(context.Background(), cfg, modelReference{})
	if err != nil || got.MaxTokens != 0 || got.MaxContextTokens != 0 || got.MaxHistoryMessages != 0 {
		t.Fatalf("legacy values survived: %+v %v", got, err)
	}
	saveConfig(req)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"maxTokens", "maxContextTokens", "maxHistoryMessages"} {
		if _, ok := stored[key]; ok {
			t.Fatalf("legacy field saved: %s", key)
		}
	}
}

func TestModelContextConcurrentWritersAndFailedSave(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	saveContextFixture(t, app, "profile", "m", "https://example.invalid/v1", ModelContextValues{MaxOutputTokens: 1024})
	ref := ModelReference{Kind: "custom", Name: "profile"}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(kind int) {
			defer wg.Done()
			for n := 0; n < 8; n++ {
				var err error
				if kind == 0 {
					_, err = app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxContextTokens: contextInt(0)}})
				} else {
					err = updateRawConfig(func(cfg *desktopConfig) error { cfg.SkipLogin = true; cfg.TenantID = "tenant"; return nil })
				}
				if err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	cfg := loadRawConfig()
	if !cfg.SkipLogin || cfg.TenantID != "tenant" || !reflect.DeepEqual(modelContextOverride(cfg, ref).MaxContextTokens, contextInt(0)) {
		t.Fatal("concurrent save lost unrelated fields")
	}
	// A read-only settings operation must not mutate an existing engine snapshot.
	before := engine.Config{MaxTokens: 777, MaxContextTokens: 888}
	app.config = before
	if _, err := app.SaveModelContextSettings(SaveModelContextSettingsRequest{Model: ref, Overrides: ModelContextOverride{MaxOutputTokens: contextInt(0)}}); err == nil {
		t.Fatal("zero output accepted")
	}
	if app.config.MaxTokens != 777 || app.config.MaxContextTokens != 888 {
		t.Fatal("preference save changed a runtime snapshot")
	}
}
