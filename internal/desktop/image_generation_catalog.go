package desktop

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	appwire "github.com/wt68/runcode/internal/protocol"
)

// 生图模型目录：哪些平台模型能生成图片、用什么参数，只认网关的 image_generation
// 扩展（设计 6.1）。不按模型名猜，也不把 supports_images（图片输入）当成图片输出。

type ImageGenerationProfile = appwire.ImageGenerationProfile

// Capability values a catalog entry may declare. A model with image_generation
// but without chat must never be offered for any text purpose.
const (
	capabilityChat            = "chat"
	capabilityImageGeneration = "image_generation"
)

const (
	imageGenerationMaxPixels      = 16777216
	imageGenerationCatalogTimeout = 10 * time.Second
	imageGenerationScopeVersion   = "image-generation-v1"
)

var (
	errImageGenerationIdentity = errors.New("无法确认当前账号的稳定身份，图片生成暂不可用；请重新登录通行证")
	errImageGenerationLogin    = errors.New("请先登录通行证后使用图片生成")
)

// isChatModel reports whether a catalog entry may serve any text purpose. Only
// an explicit image_generation-without-chat declaration removes a model; an
// old catalog without capabilities keeps its chat behavior.
func isChatModel(m PassportModel) bool {
	if len(m.Capabilities) == 0 {
		return true
	}
	return slices.Contains(m.Capabilities, capabilityChat) ||
		!slices.Contains(m.Capabilities, capabilityImageGeneration)
}

// isImageGenerationModel reports whether the gateway declared image generation.
// Whether it is selectable is decided by its normalized profile.
func isImageGenerationModel(m PassportModel) bool {
	return slices.Contains(m.Capabilities, capabilityImageGeneration)
}

func parseImageSize(size string) (int, int, bool) {
	w, h, ok := strings.Cut(strings.TrimSpace(size), "x")
	if !ok {
		return 0, 0, false
	}
	width, err1 := strconv.Atoi(w)
	height, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 || width > 16384 || height > 16384 {
		return 0, 0, false
	}
	return width, height, width*height <= imageGenerationMaxPixels
}

// normalizeImageGenerationProfile copies a declared profile and marks it
// unavailable when it cannot be used safely: a selectable profile must name
// its sizes, a default among them, and only response formats this client
// can store.
func normalizeImageGenerationProfile(p *ImageGenerationProfile) ImageGenerationProfile {
	if p == nil {
		return ImageGenerationProfile{SupportedSizes: []string{}, ResponseFormats: []string{}, UnavailableReason: "模型目录未提供生图参数，暂不可用"}
	}
	out := ImageGenerationProfile{
		Available:         p.Available,
		SupportedSizes:    []string{},
		DefaultSize:       strings.TrimSpace(p.DefaultSize),
		ResponseFormats:   []string{},
		UnavailableReason: strings.TrimSpace(p.UnavailableReason),
	}
	invalid := false
	for _, size := range p.SupportedSizes {
		size = strings.TrimSpace(size)
		if _, _, ok := parseImageSize(size); !ok {
			invalid = true
			continue
		}
		if !slices.Contains(out.SupportedSizes, size) {
			out.SupportedSizes = append(out.SupportedSizes, size)
		}
	}
	for _, format := range p.ResponseFormats {
		format = strings.TrimSpace(format)
		if format != "url" && format != "b64_json" {
			invalid = true
			continue
		}
		if !slices.Contains(out.ResponseFormats, format) {
			out.ResponseFormats = append(out.ResponseFormats, format)
		}
	}
	if invalid || len(out.SupportedSizes) == 0 || len(out.ResponseFormats) == 0 || !slices.Contains(out.SupportedSizes, out.DefaultSize) {
		if out.Available {
			out.UnavailableReason = "模型目录的生图参数无效，暂不可用"
		}
		out.Available = false
	}
	if !out.Available && out.UnavailableReason == "" {
		out.UnavailableReason = "该生图模型当前不可用"
	}
	if out.Available {
		out.UnavailableReason = ""
	}
	return out
}

// imageGenerationCandidates lists every declared image model of the catalog,
// pinned to the current Bridge and the given tenant, in catalog order.
func imageGenerationCandidates(models []PassportModel, tenantID string) []appwire.ImageGenerationModel {
	out := []appwire.ImageGenerationModel{}
	seen := map[string]bool{}
	for _, m := range models {
		if !isImageGenerationModel(m) || strings.TrimSpace(m.ID) == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, appwire.ImageGenerationModel{
			Model:    platformModelRef(tenantID, m.ID),
			Provider: m.OwnedBy,
			Profile:  normalizeImageGenerationProfile(m.ImageGeneration),
		})
	}
	return out
}

func (c modelCatalog) imageGenerationModels() []PassportModel {
	out := make([]PassportModel, 0, len(c.imageOrder))
	for _, id := range c.imageOrder {
		out = append(out, c.imageGeneration[id])
	}
	return out
}

// imageGenerationAccount is the stable account fingerprint that partitions
// image generation state on disk, plus the in-memory login epoch.
func (a *App) imageGenerationAccount() (account, epoch string, err error) {
	if a.tokens == nil || !a.tokens.LoggedIn() {
		return "", "", errImageGenerationLogin
	}
	a.tokens.mu.Lock()
	token, epoch := a.tokens.ts.AccessToken, a.tokens.identity
	a.tokens.mu.Unlock()
	account = tokenIdentity(token)
	if account == "" {
		return "", epoch, errImageGenerationIdentity
	}
	return account, epoch, nil
}

func imageGenerationBridge() string {
	return strings.TrimRight(passportConfig().BridgeBaseURL, "/")
}

// imageGenerationScope partitions settings and session switches by account,
// Bridge and tenant: switching tenant or account never reuses another
// partition's paid model choice.
func imageGenerationScope(account, bridge, tenantID string) string {
	return digestVision([]string{imageGenerationScopeVersion, account, strings.TrimRight(bridge, "/"), strings.TrimSpace(tenantID)})
}

// freshImageGenerationCatalog always asks the Bridge, so a manual refresh and
// the pre-submit check never answer from a cache entry.
func (a *App) freshImageGenerationCatalog(ctx context.Context, tenantID, actor string) (modelCatalog, error) {
	scope := platformModelRef(tenantID, "")
	if _, err := a.passportModelsContext(ctx, tenantID); err != nil {
		return modelCatalog{}, err
	}
	if a.visionAccountIdentity() != actor {
		return modelCatalog{}, errors.New("加载模型目录期间账号已变化，请重试")
	}
	a.mu.Lock()
	entry, ok := a.modelCatalogs[modelCatalogKey(scope, actor)]
	a.mu.Unlock()
	if !ok {
		return modelCatalog{}, errors.New("平台模型目录已变化，请重试")
	}
	return entry, nil
}

// ListImageGenerationModels lists the image models the current account may
// see under tenantID. A failed lookup is an error, never an empty catalog.
func (a *App) ListImageGenerationModels(tenantID string) (appwire.ImageGenerationCatalog, error) {
	tenantID = strings.TrimSpace(tenantID)
	account, _, err := a.imageGenerationAccount()
	if err != nil {
		return appwire.ImageGenerationCatalog{}, wireError(err)
	}
	actor := a.visionAccountIdentity()
	ctx, cancel := context.WithTimeout(context.Background(), imageGenerationCatalogTimeout)
	defer cancel()
	catalog, err := a.freshImageGenerationCatalog(ctx, tenantID, actor)
	if err != nil {
		return appwire.ImageGenerationCatalog{}, wireError(err)
	}
	if again, _, err := a.imageGenerationAccount(); err != nil || again != account {
		return appwire.ImageGenerationCatalog{}, wireError(errors.New("加载模型目录期间账号已变化，请重试"))
	}
	return appwire.ImageGenerationCatalog{
		ScopeID: imageGenerationScope(account, imageGenerationBridge(), tenantID),
		Models:  imageGenerationCandidates(catalog.imageGenerationModels(), tenantID),
	}, nil
}
