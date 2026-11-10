package protocol

// 文生图（IMG-GEN-001）的公共契约。字段与语义以《文生图工具接入设计说明》第 10 节
// 为唯一依据；网关 HTTP 原文是 snake_case，这里全部是 camelCase，两者不混用。
// 状态、错误码与记账观察的取值集合见设计 10.1/6.4，由宿主私有常量与前端
// core/image-generation.ts 各自声明。

// ImageGenerationProfile is the gateway-declared parameter profile of one
// image model. Available is false for combinations not yet verified, with
// UnavailableReason saying why.
type ImageGenerationProfile struct {
	Available         bool     `json:"available"`
	SupportedSizes    []string `json:"supportedSizes"`
	DefaultSize       string   `json:"defaultSize"`
	ResponseFormats   []string `json:"responseFormats"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
}

// ImageGenerationModel is one candidate in the settings catalog.
type ImageGenerationModel struct {
	Model    ModelReference         `json:"model"`
	Provider string                 `json:"provider"`
	Profile  ImageGenerationProfile `json:"profile"`
}

// ImageGenerationCatalog lists the image models of one account/Bridge/tenant
// scope. An empty Models is a real "no models"; a failed lookup is an error.
type ImageGenerationCatalog struct {
	ScopeID string                 `json:"scopeId"`
	Models  []ImageGenerationModel `json:"models"`
}

// ImageGenerationSettings is the saved default of one scope.
type ImageGenerationSettings struct {
	ScopeID      string          `json:"scopeId"`
	Revision     int64           `json:"revision"`
	DefaultModel *ModelReference `json:"defaultModel,omitempty"`
	DefaultSize  string          `json:"defaultSize"`
}

// SaveImageGenerationSettingsRequest replaces one scope's default. A nil
// DefaultModel clears it. It carries no Bridge or account field: the host
// derives the scope from the current identity and rejects a stale ScopeID.
type SaveImageGenerationSettingsRequest struct {
	TenantID         string          `json:"tenantId"`
	ScopeID          string          `json:"scopeId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	DefaultModel     *ModelReference `json:"defaultModel,omitempty"`
	DefaultSize      string          `json:"defaultSize"`
}

// SessionImageGenerationView is one session's switch. Enabled is what the user
// chose; Available says whether the next turn can actually generate, with
// Reason when it cannot.
type SessionImageGenerationView struct {
	SessionID string          `json:"sessionId"`
	Enabled   bool            `json:"enabled"`
	Revision  int64           `json:"revision"`
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	Model     *ModelReference `json:"model,omitempty"`
	Size      string          `json:"size,omitempty"`
}

// SetSessionImageGenerationRequest changes one session's switch with a revision check.
type SetSessionImageGenerationRequest struct {
	SessionID        string `json:"sessionId"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// ImageGenerationArtifact is one saved image, addressed relative to the workspace.
type ImageGenerationArtifact struct {
	Path     string `json:"path"`
	MIMEType string `json:"mimeType"`
	Bytes    int64  `json:"bytes"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// ImageGenerationResult is the bounded result shared by the model's tool
// content, the live tool event and history replay. It never carries image
// bytes, Base64, signed URLs or credentials. Status is one of succeeded /
// failed / unknown / cancelled / save_failed; AccountingStatus is one of
// verified / unverified / ambiguous / observed_mismatch and is independent of
// Status — an automatic result is always unverified.
type ImageGenerationResult struct {
	Version          int                       `json:"version"`
	GenerationID     string                    `json:"generationId"`
	Status           string                    `json:"status"`
	Code             string                    `json:"code,omitempty"`
	Model            string                    `json:"model"`
	Size             string                    `json:"size"`
	Images           []ImageGenerationArtifact `json:"images"`
	Message          string                    `json:"message,omitempty"`
	RequestID        string                    `json:"requestId,omitempty"`
	AccountingStatus string                    `json:"accountingStatus"`
}
