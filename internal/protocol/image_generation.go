package protocol

// 文生图（IMG-GEN-001）的公共契约。字段与语义以《文生图工具接入设计说明》第 10 节
// 为唯一依据；网关 HTTP 原文是 snake_case，这里全部是 camelCase，两者不混用。
// 状态、错误码与记账观察的取值集合见设计 10.1/6.4，由宿主私有常量与前端
// core/image-generation.ts 各自声明。

// ImageGenerationProfile 是网关声明的单模型参数档案。
// Available=false 表示组合尚未 verified，UnavailableReason 说明原因。
type ImageGenerationProfile struct {
	Available         bool     `json:"available"`
	SupportedSizes    []string `json:"supportedSizes"`
	DefaultSize       string   `json:"defaultSize"`
	ResponseFormats   []string `json:"responseFormats"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
}

// ImageGenerationModel 是设置页目录中的一条候选。
type ImageGenerationModel struct {
	Model    ModelReference         `json:"model"`
	Provider string                 `json:"provider"`
	Profile  ImageGenerationProfile `json:"profile"`
}

// ImageGenerationCatalog 列出某一账号／Bridge／租户作用域下的生图模型。
// Models 为空表示「确实没有」；查询失败必须是错误，不能冒充空目录。
type ImageGenerationCatalog struct {
	ScopeID string                 `json:"scopeId"`
	Models  []ImageGenerationModel `json:"models"`
}

// ImageGenerationSettings 是某一作用域已保存的默认项。
type ImageGenerationSettings struct {
	ScopeID      string          `json:"scopeId"`
	Revision     int64           `json:"revision"`
	DefaultModel *ModelReference `json:"defaultModel,omitempty"`
	DefaultSize  string          `json:"defaultSize"`
}

// SaveImageGenerationSettingsRequest 替换某一作用域的默认。DefaultModel 为 nil
// 表示清除。不带 Bridge／账号字段：宿主从当前身份推导作用域，并拒绝过期 ScopeID。
type SaveImageGenerationSettingsRequest struct {
	TenantID         string          `json:"tenantId"`
	ScopeID          string          `json:"scopeId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	DefaultModel     *ModelReference `json:"defaultModel,omitempty"`
	DefaultSize      string          `json:"defaultSize"`
}

// SessionImageGenerationView 是单会话开关视图。Enabled 是用户选择；Available
// 表示下一回合能否真正生成，不能时带 Reason。
type SessionImageGenerationView struct {
	SessionID string          `json:"sessionId"`
	Enabled   bool            `json:"enabled"`
	Revision  int64           `json:"revision"`
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	Model     *ModelReference `json:"model,omitempty"`
	Size      string          `json:"size,omitempty"`
}

// SetSessionImageGenerationRequest 带修订号检查地改写单会话开关。
type SetSessionImageGenerationRequest struct {
	SessionID        string `json:"sessionId"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// ImageGenerationArtifact 是一张已保存图片，路径相对工作区。
type ImageGenerationArtifact struct {
	Path     string `json:"path"`
	MIMEType string `json:"mimeType"`
	Bytes    int64  `json:"bytes"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// 生图请求的本地提交边界。它只回答客户端是否进入 HTTP Do，不承诺网络字节
// 是否送达、Bridge/Core 是否收到、图片是否生成或是否扣费。
const (
	ImageGenerationSubmissionNotSubmitted = "not_submitted"
	ImageGenerationSubmissionAttempted    = "attempted"
	ImageGenerationSubmissionUnknown      = "unknown"
)

// ImageGenerationResult 是模型工具正文、现场工具事件与历史回放共用的有界结果。
// 从不携带图片字节、Base64、签名 URL 或凭据。Status 为 succeeded／failed／
// unknown／cancelled／save_failed；SubmissionStatus 表示本地是否进入发送尝试；
// AccountingStatus 为 verified／unverified／ambiguous／observed_mismatch，与前两者
// 独立——自动路径一律 unverified，历史缺提交字段按 unknown。
type ImageGenerationResult struct {
	Version          int                       `json:"version"`
	GenerationID     string                    `json:"generationId"`
	Status           string                    `json:"status"`
	Code             string                    `json:"code,omitempty"`
	Model            string                    `json:"model"`
	Size             string                    `json:"size"`
	Images           []ImageGenerationArtifact `json:"images"`
	Message          string                    `json:"message,omitempty"`
	DisplayIntent    string                    `json:"displayIntent,omitempty"` // 清洗后一句话意图；出图前／回放用，不含完整 prompt
	RequestID        string                    `json:"requestId,omitempty"`
	SubmissionStatus string                    `json:"submissionStatus,omitempty"`
	AccountingStatus string                    `json:"accountingStatus"`
}
