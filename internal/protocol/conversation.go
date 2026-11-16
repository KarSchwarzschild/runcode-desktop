package protocol

// ConversationInfo 是桌面会话显示契约，Kind 区分普通引擎和宿主知识库问答。
// 保留既有引擎 SessionInfo 的显示字段，新增桌面模式无需修改引擎协议。
type ConversationInfo struct {
	Kind string `json:"kind,omitempty"`
	// ReadOnlyReason 仅展示安全的不可继续原因，不能用于绕过后端权限检查。
	ReadOnlyReason    string `json:"readOnlyReason,omitempty"`
	SessionID         string `json:"sessionId"`
	Model             string `json:"model"`
	CWD               string `json:"cwd"`
	PermissionMode    string `json:"permissionMode"`
	PlanMode          bool   `json:"planMode"`
	ReasoningScenario string `json:"reasoningScenario"`
	ThinkingEffort    string `json:"thinkingEffort"`
	// MaxContextTokens is the compaction budget (0 = auto-compaction off), so the UI
	// can show a context-usage bar and how close a turn is to triggering compaction.
	MaxContextTokens   int     `json:"maxContextTokens"`
	InputPricePerMTok  float64 `json:"inputPricePerMTok"`
	OutputPricePerMTok float64 `json:"outputPricePerMTok"`
	PricingSource      string  `json:"pricingSource"`
	// PreviewBaseURL is the loopback static-server root for previewing workspace
	// files (empty if the server could not start). e.g. "http://127.0.0.1:52713/".
	PreviewBaseURL string `json:"previewBaseURL"`
}
