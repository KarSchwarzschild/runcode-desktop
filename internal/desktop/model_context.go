package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appwire "github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
)

const maxModelContextValue = 1<<31 - 1

func validateModelContextOverride(v ModelContextOverride) error {
	for _, field := range []struct {
		name  string
		value *int
		min   int
	}{
		{"最大输出 Tokens", v.MaxOutputTokens, 1},
		{"上下文预算", v.MaxContextTokens, 0},
		{"历史消息上限", v.MaxHistoryMessages, 0},
	} {
		if field.value != nil && (*field.value < field.min || *field.value > maxModelContextValue) {
			return fmt.Errorf("%s 必须为 %d 到 %d 的整数", field.name, field.min, maxModelContextValue)
		}
	}
	return nil
}

func mergeModelContext(defaults *ModelContextValues, overrides ModelContextOverride) ModelContextValues {
	var value ModelContextValues
	if defaults != nil {
		value = *defaults
	}
	if overrides.MaxOutputTokens != nil {
		value.MaxOutputTokens = *overrides.MaxOutputTokens
	}
	if overrides.MaxContextTokens != nil {
		value.MaxContextTokens = *overrides.MaxContextTokens
	}
	if overrides.MaxHistoryMessages != nil {
		value.MaxHistoryMessages = *overrides.MaxHistoryMessages
	}
	return value
}

func modelContextOverride(stored desktopConfig, ref modelReference) ModelContextOverride {
	for _, item := range stored.ModelContexts {
		if item.Model == ref {
			return item.Overrides
		}
	}
	return ModelContextOverride{}
}

func emptyModelContextOverride(v ModelContextOverride) bool {
	return v.MaxOutputTokens == nil && v.MaxContextTokens == nil && v.MaxHistoryMessages == nil
}

func customModelExists(cfg desktopConfig, name string) bool {
	for _, m := range cfg.CustomModels {
		if m.Name == name {
			return true
		}
	}
	return false
}

// Defaults remain separate from local overrides; neither the catalog nor another
// model's runtime config may become an implicit fallback.
func (a *App) modelContextView(ctx context.Context, ref modelReference, actor string) (ModelContextView, error) {
	return modelContextViewFrom(ref, func(scope modelReference) (modelCatalog, error) {
		return a.platformModelCatalog(ctx, scope, actor)
	})
}

func modelContextViewFrom(ref modelReference, catalogOf func(scope modelReference) (modelCatalog, error)) (ModelContextView, error) {
	view := ModelContextView{Model: ref}
	if ref.Name == "" || strings.TrimSpace(ref.Name) != ref.Name {
		return view, errors.New("请选择模型")
	}
	switch ref.Kind {
	case "custom":
		if ref.Bridge != "" || ref.TenantID != "" || !customModelExists(loadRawConfig(), ref.Name) {
			return view, errors.New("自定义模型不存在，请刷新模型列表")
		}
		view.Warning = "自定义连接未声明平台默认值；未覆盖的项使用引擎或供应商的默认行为。"
	case "platform":
		if ref != platformModelRef(ref.TenantID, ref.Name) {
			return view, errors.New("模型所属平台已变化，请刷新模型列表")
		}
		scope := ref
		scope.Name = ""
		catalog, err := catalogOf(scope)
		if err != nil {
			return view, err
		}
		m, ok := catalog.models[ref.Name]
		if !ok {
			return view, errors.New("当前租户无权使用此模型，或模型已从平台移除")
		}
		if m.MaxOutputTokens != nil && m.MaxHistoryMessages != nil {
			v := ModelContextValues{MaxOutputTokens: *m.MaxOutputTokens, MaxContextTokens: m.ContextTokens, MaxHistoryMessages: *m.MaxHistoryMessages}
			if err := validateModelContextOverride(ModelContextOverride{MaxOutputTokens: &v.MaxOutputTokens, MaxContextTokens: &v.MaxContextTokens, MaxHistoryMessages: &v.MaxHistoryMessages}); err != nil {
				return view, fmt.Errorf("平台模型预算无效: %w", err)
			}
			view.Defaults = &v
		} else {
			view.Warning = "Bridge 未提供完整的逐模型上下文配置，请升级服务端；也可在本机明确覆盖全部三项后使用。"
		}
	default:
		return view, errors.New("不支持的模型类型")
	}
	view.Overrides = modelContextOverride(loadRawConfig(), ref)
	if err := validateModelContextOverride(view.Overrides); err != nil {
		return view, err
	}
	view.Effective = mergeModelContext(view.Defaults, view.Overrides)
	return view, nil
}

// GetModelContextSettings reads defaults and local overrides for one model.
func (a *App) GetModelContextSettings(ref ModelReference) (ModelContextView, error) {
	if ref.Kind == "platform" && ref.Bridge == "" {
		ref = platformModelRef(ref.TenantID, ref.Name)
	}
	view, err := a.modelContextView(context.Background(), ref, a.visionAccountIdentity())
	return view, wireError(err)
}

// SaveModelContextSettings only persists a preference; it never interrupts a turn.
func (a *App) SaveModelContextSettings(req SaveModelContextSettingsRequest) (ModelContextView, error) {
	if err := validateModelContextOverride(req.Overrides); err != nil {
		return ModelContextView{}, wireError(err)
	}
	actor := a.visionAccountIdentity()
	view, err := a.modelContextView(context.Background(), req.Model, actor)
	if err != nil {
		return ModelContextView{}, wireError(err)
	}
	err = updateRawConfig(func(cfg *desktopConfig) error {
		if req.Model.Kind == "platform" && (a.visionAccountIdentity() != actor || !a.tokens.LoggedIn() || req.Model != platformModelRef(req.Model.TenantID, req.Model.Name)) {
			return errors.New("账号或平台已变化，请刷新模型列表")
		}
		if req.Model.Kind == "custom" && !customModelExists(*cfg, req.Model.Name) {
			return errors.New("自定义模型已变化，请刷新模型列表")
		}
		next := make([]appwire.ModelContextPreference, 0, len(cfg.ModelContexts)+1)
		for _, item := range cfg.ModelContexts {
			if item.Model != req.Model {
				next = append(next, item)
			}
		}
		if !emptyModelContextOverride(req.Overrides) {
			if len(next) >= 512 {
				return errors.New("本机模型覆盖数量已达上限，请先恢复不再使用的模型默认值")
			}
			next = append(next, appwire.ModelContextPreference(req))
		}
		cfg.ModelContexts = next
		return nil
	})
	if err != nil {
		return ModelContextView{}, wireError(err)
	}
	view.Overrides = req.Overrides
	view.Effective = mergeModelContext(view.Defaults, req.Overrides)
	return view, nil
}

func (a *App) applyModelContext(ctx context.Context, cfg engine.Config, ref modelReference) (engine.Config, error) {
	// Unnamed direct connections have no saved profile; never inherit a prior
	// platform model's budgets or the abandoned desktop-wide settings.
	value := ModelContextValues{}
	if ref.Kind == "platform" || ref.Kind == "custom" && ref.Name != "" {
		actor := a.visionAccountIdentity()
		// A Bridge outage must not lock the user out of opening or resuming a
		// conversation: buildModelCatalog falls back to this account's last
		// snapshot, else local overrides that set all three values. Budgets are
		// fixed for the session's lifetime, so an unset field here is not "use
		// the engine default": MaxContextTokens 0 switches compaction off even
		// after the Bridge comes back. Partial overrides therefore keep failing.
		view, err := modelContextViewFrom(ref, func(scope modelReference) (modelCatalog, error) {
			return a.buildModelCatalog(ctx, scope, actor)
		})
		if errors.Is(err, errModelCatalogUnavailable) {
			overrides := modelContextOverride(loadRawConfig(), ref)
			if overrides.MaxOutputTokens == nil || overrides.MaxContextTokens == nil || overrides.MaxHistoryMessages == nil {
				return cfg, err
			}
			if err := validateModelContextOverride(overrides); err != nil {
				return cfg, err
			}
			debugLog("model context: %v; building %q from local overrides", err, ref.Name)
			value = mergeModelContext(nil, overrides)
			cfg.MaxTokens, cfg.MaxContextTokens, cfg.MaxHistoryMessages = value.MaxOutputTokens, value.MaxContextTokens, value.MaxHistoryMessages
			return cfg, nil
		}
		if err != nil {
			return cfg, err
		}
		if ref.Kind == "platform" && view.Defaults == nil && (view.Overrides.MaxOutputTokens == nil || view.Overrides.MaxContextTokens == nil || view.Overrides.MaxHistoryMessages == nil) {
			return cfg, errors.New(view.Warning)
		}
		value = view.Effective
	}
	cfg.MaxTokens, cfg.MaxContextTokens, cfg.MaxHistoryMessages = value.MaxOutputTokens, value.MaxContextTokens, value.MaxHistoryMessages
	return cfg, nil
}
