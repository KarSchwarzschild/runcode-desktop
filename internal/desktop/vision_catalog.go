package desktop

import (
	"context"
	"errors"
	"fmt"
)

func selectPlatformVisionModel(defaults []PassportModel) (PassportModel, error) {
	if len(defaults) == 0 {
		return PassportModel{}, errors.New("平台未配置默认识图模型，或当前租户无权使用；请联系管理员或手动选择识图模型")
	}
	if len(defaults) != 1 {
		return PassportModel{}, errors.New("平台配置了多个默认识图模型，请联系管理员修正")
	}
	m := defaults[0]
	if m.ID == "" || !m.VisionDefault || m.SupportsImages == nil || !*m.SupportsImages {
		return PassportModel{}, errors.New("平台默认识图模型必须明确支持图片")
	}
	return m, nil
}

// Explicit user preferences never silently fall back to another connection.
func (a *App) visionTarget(ctx context.Context, sessionID, bridge, actor string, source modelReference, stored desktopConfig) (modelReference, error) {
	if stored.Vision.Disabled {
		return modelReference{}, errors.New("已关闭识图兜底，当前模型仅支持文本；请在设置中选择跟随平台或指定识图模型")
	}
	if stored.Vision.DefaultModel != nil {
		return *stored.Vision.DefaultModel, nil
	}
	scope := source
	scope.Name = ""
	if source.Kind != "platform" {
		a.mu.Lock()
		entry := a.entryLocked(sessionID)
		tenant, bound := "", entry != nil
		if bound {
			tenant = entry.tenantID
		}
		a.mu.Unlock()
		if !bound {
			return modelReference{}, errors.New("会话没有平台租户绑定，请重新打开会话或手动选择识图模型")
		}
		scope = modelReference{Kind: "platform", Bridge: bridge, TenantID: tenant}
	}
	catalog, err := a.platformModelCatalog(ctx, scope, actor)
	if err != nil {
		return modelReference{}, err
	}
	model, err := selectPlatformVisionModel(catalog.visionDefaults)
	if err != nil {
		return modelReference{}, err
	}
	scope.Name = model.ID
	if override := imageOverride(stored, scope); override != nil && !*override {
		return modelReference{}, fmt.Errorf("平台默认识图模型 %s 已被本机标为仅文本，请恢复其图片能力或手动选择其他识图模型", model.ID)
	}
	return scope, nil
}
