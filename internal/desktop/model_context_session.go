package desktop

import (
	"context"
	"fmt"
	"strings"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/sessions"
)

// Resolve persisted model identity before building the engine: host applies meta
// after Build, which is too late to select the matching construction-time limits.
func restoredContextModel(ctx context.Context, cfg engine.Config, restoreModel bool) (engine.Config, error) {
	if cfg.Resume == "" && cfg.SessionID == "" && !cfg.Continue {
		return cfg, nil
	}
	backend, err := sessions.OpenBackend(cfg.CWD, cfg.SessionBackend)
	if err != nil {
		return cfg, err
	}
	defer func() { _ = backend.Close(context.Background()) }()
	if cfg.Continue && cfg.Resume == "" {
		cfg.Resume, err = backend.Latest(ctx)
		if err != nil {
			return cfg, fmt.Errorf("读取最近会话失败: %w", err)
		}
		cfg.Continue = false
	}
	id := strings.TrimSpace(cfg.Resume)
	if id == "" {
		id = strings.TrimSpace(cfg.SessionID)
	}
	if !restoreModel || id == "" {
		return cfg, nil
	}
	meta, err := backend.LoadMeta(ctx, id)
	if err != nil {
		return cfg, fmt.Errorf("读取会话模型失败: %w", err)
	}
	if meta.Model != "" {
		cfg.Model = meta.Model
	}
	return cfg, nil
}

// keepRestoredPlatformModel decides whether a passport build may adopt the model
// recorded in the session sidecar. The sidecar has no connection identity, so a
// conversation last run on a custom connection carries that connection's model
// id; asking the platform for it would fail the resume outright. Only a catalog
// that answered can reject the id — an unreachable catalog keeps it, and the
// budget resolution reports anything still wrong.
func (a *App) keepRestoredPlatformModel(ctx context.Context, tenantID, model string) bool {
	actor := a.visionAccountIdentity()
	catalog, err := a.buildModelCatalog(ctx, platformModelRef(tenantID, ""), actor)
	if err != nil {
		return true
	}
	_, ok := catalog.models[model]
	return ok
}
