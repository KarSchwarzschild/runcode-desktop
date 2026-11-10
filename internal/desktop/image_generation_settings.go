package desktop

import (
	"context"
	"errors"
	"slices"
	"strings"

	appwire "github.com/wt68/runcode/internal/protocol"
)

// 默认生图模型与尺寸：按「账号指纹 + Bridge + 租户」分区保存在 desktop.json 里，
// 不保存任何凭据。显式保存；保存时复核目录与权限，读取不发网络请求。

// imageGenerationPreference is one scope's saved default. SupportedSizes is
// the catalog profile validated at save time, so a turn can bound the model's
// size argument without a catalog round-trip; the pre-submit check still
// revalidates against the live catalog.
type imageGenerationPreference struct {
	Revision       int64           `json:"revision"`
	DefaultModel   *modelReference `json:"defaultModel,omitempty"`
	DefaultSize    string          `json:"defaultSize,omitempty"`
	SupportedSizes []string        `json:"supportedSizes,omitempty"`
}

func (p imageGenerationPreference) view(scope string) appwire.ImageGenerationSettings {
	out := appwire.ImageGenerationSettings{ScopeID: scope, Revision: p.Revision, DefaultSize: p.DefaultSize}
	if p.DefaultModel != nil {
		ref := *p.DefaultModel
		out.DefaultModel = &ref
	}
	return out
}

func (p imageGenerationPreference) sameTarget(model *modelReference, size string) bool {
	if (p.DefaultModel == nil) != (model == nil) {
		return false
	}
	return model == nil || (*p.DefaultModel == *model && p.DefaultSize == size)
}

func imageGenerationPreferenceFor(cfg desktopConfig, scope string) imageGenerationPreference {
	return cfg.ImageGenerationPreferences[scope]
}

// GetImageGenerationSettings returns the current account's saved default for
// tenantID. It never contacts the Bridge.
func (a *App) GetImageGenerationSettings(tenantID string) (appwire.ImageGenerationSettings, error) {
	tenantID = strings.TrimSpace(tenantID)
	account, _, err := a.imageGenerationAccount()
	if err != nil {
		return appwire.ImageGenerationSettings{}, wireError(err)
	}
	scope := imageGenerationScope(account, imageGenerationBridge(), tenantID)
	return imageGenerationPreferenceFor(loadRawConfig(), scope).view(scope), nil
}

// SaveImageGenerationSettings replaces the scope's default after checking the
// model against the live catalog. Repeating the confirmed target returns it
// unchanged; a different target with a stale revision is rejected.
func (a *App) SaveImageGenerationSettings(req appwire.SaveImageGenerationSettingsRequest) (appwire.ImageGenerationSettings, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	account, epoch, err := a.imageGenerationAccount()
	if err != nil {
		return appwire.ImageGenerationSettings{}, wireError(err)
	}
	bridge := imageGenerationBridge()
	scope := imageGenerationScope(account, bridge, tenantID)
	if req.ScopeID != scope {
		return appwire.ImageGenerationSettings{}, wireError(errors.New("账号或租户已变化，请刷新图片生成设置后重试"))
	}
	var model *modelReference
	size := strings.TrimSpace(req.DefaultSize)
	if req.DefaultModel != nil {
		ref := *req.DefaultModel
		ref.Name = strings.TrimSpace(ref.Name)
		if ref.Name == "" || ref != platformModelRef(tenantID, ref.Name) {
			return appwire.ImageGenerationSettings{}, wireError(errors.New("无效的生图模型引用，请从当前租户的生图模型中选择"))
		}
		model = &ref
	} else {
		size = ""
	}
	current := imageGenerationPreferenceFor(loadRawConfig(), scope)
	if current.sameTarget(model, size) {
		return current.view(scope), nil
	}
	if current.Revision != req.ExpectedRevision {
		return appwire.ImageGenerationSettings{}, wireError(errors.New("图片生成设置已变化，请刷新后重试"))
	}
	var supported []string
	if model != nil {
		actor := a.visionAccountIdentity()
		ctx, cancel := context.WithTimeout(context.Background(), imageGenerationCatalogTimeout)
		catalog, err := a.freshImageGenerationCatalog(ctx, tenantID, actor)
		cancel()
		if err != nil {
			return appwire.ImageGenerationSettings{}, wireError(err)
		}
		entry, ok := catalog.imageGeneration[model.Name]
		if !ok {
			return appwire.ImageGenerationSettings{}, wireError(errors.New("该生图模型不在当前租户的可用目录中"))
		}
		profile := normalizeImageGenerationProfile(entry.ImageGeneration)
		if !profile.Available {
			return appwire.ImageGenerationSettings{}, wireError(errors.New("该生图模型当前不可用：" + profile.UnavailableReason))
		}
		if size == "" {
			size = profile.DefaultSize
		}
		if !slices.Contains(profile.SupportedSizes, size) {
			return appwire.ImageGenerationSettings{}, wireError(errors.New("该生图模型不支持所选尺寸"))
		}
		supported = profile.SupportedSizes
		if current.sameTarget(model, size) {
			return current.view(scope), nil
		}
	}
	if again, againEpoch, err := a.imageGenerationAccount(); err != nil || again != account || againEpoch != epoch {
		return appwire.ImageGenerationSettings{}, wireError(errors.New("保存期间账号已变化，请重试"))
	}
	var saved imageGenerationPreference
	err = updateRawConfig(func(cfg *desktopConfig) error {
		prev := cfg.ImageGenerationPreferences[scope]
		if prev.Revision != req.ExpectedRevision {
			return errors.New("图片生成设置已变化，请刷新后重试")
		}
		saved = imageGenerationPreference{Revision: prev.Revision + 1, DefaultModel: model, DefaultSize: size, SupportedSizes: supported}
		if cfg.ImageGenerationPreferences == nil {
			cfg.ImageGenerationPreferences = map[string]imageGenerationPreference{}
		}
		cfg.ImageGenerationPreferences[scope] = saved
		return nil
	})
	if err != nil {
		return appwire.ImageGenerationSettings{}, wireError(err)
	}
	return saved.view(scope), nil
}
