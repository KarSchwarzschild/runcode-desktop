package protocol

// 邮箱账号设置的公共契约（接入向导 / 设置小节）。密文只经请求进入，
// 响应与 SettingsView 永不携带密码或 PasswordProtected。

// MailAccountVersion 是阿里企业邮箱服务预设。
const (
	MailVersionStandard = "standard" // smtp.qiye.aliyun.com
	MailVersionXC       = "xc"       // smtp.xc.aliyun.com（国产化）
	MailVersionHK       = "hk"       // smtphk.qiye.aliyun.com
)

// MailOnboardingStatus 是接入进度的事实状态，不是单一 verified 布尔值。
const (
	MailStatusNotConfigured        = "not_configured"
	MailStatusGuiding              = "guiding"
	MailStatusConnectionTestPassed = "connection_test_passed"
	MailStatusSaved                = "saved"
	MailStatusTestMailAccepted     = "test_mail_accepted"
	MailStatusReceiptConfirmed     = "receipt_confirmed"
	MailStatusNeedsAttention       = "needs_attention"
)

// MailSettings 是设置页加载的脱敏视图。
type MailSettings struct {
	Account *MailAccountView `json:"account,omitempty"`
	// SecretStorageOK 复用系统钥匙串探测结论，前端据此禁用“安全保存”。
	SecretStorageOK bool `json:"secretStorageOk"`
}

// MailAccountView 是已保存账号的公开视图；无密码字段。
type MailAccountView struct {
	ID                   string `json:"id"`
	Address              string `json:"address"`
	DisplayName          string `json:"displayName"`
	Version              string `json:"version"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Enabled              bool   `json:"enabled"`
	HasPassword          bool   `json:"hasPassword"`
	Revision             int64  `json:"revision"`
	CredentialRevision   int64  `json:"credentialRevision"`
	OnboardingStatus     string `json:"onboardingStatus"`
	LastConnectionTestAt string `json:"lastConnectionTestAt,omitempty"`
	TestMailAcceptedAt   string `json:"testMailAcceptedAt,omitempty"`
	ReceiptConfirmedAt   string `json:"receiptConfirmedAt,omitempty"`
	LastTestShortID      string `json:"lastTestShortId,omitempty"`
	LastTestSubmissionID string `json:"lastTestSubmissionId,omitempty"`
}

// SaveMailAccountRequest 保存当前表单。Password 留空保留旧值；ClearPassword 才清空。
// ConfigFingerprint 必须来自同配置的 TestMailConnection 成功结果。
type SaveMailAccountRequest struct {
	Address           string `json:"address"`
	DisplayName       string `json:"displayName"`
	Version           string `json:"version"`
	Password          string `json:"password,omitempty"`
	ClearPassword     bool   `json:"clearPassword,omitempty"`
	ConfigFingerprint string `json:"configFingerprint"`
	ExpectedRevision  int64  `json:"expectedRevision"`
	Enabled           bool   `json:"enabled"`
}

// DisableMailAccountRequest 禁用已保存账号（保留配置，停止发信）。
type DisableMailAccountRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

// RemoveMailAccountRequest 删除已保存账号与密文。
type RemoveMailAccountRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

// TestMailConnectionRequest 使用当前表单值探测 SMTP，不落盘、不发信。
type TestMailConnectionRequest struct {
	Address  string `json:"address"`
	Password string `json:"password"`
	Version  string `json:"version"`
}

// MailConnectionTestResult 是连接测试结果；成功时带 ConfigFingerprint 供保存校验。
type MailConnectionTestResult struct {
	OK                bool   `json:"ok"`
	Phase             string `json:"phase,omitempty"` // connecting|tls|auth|done
	TestedAtUTC       string `json:"testedAtUtc,omitempty"`
	ConfigFingerprint string `json:"configFingerprint,omitempty"`
	ErrorCategory     string `json:"errorCategory,omitempty"`
	Message           string `json:"message"`
}

// SendMailTestToSelfRequest 向已保存账号自身发固定测试信；无收件人字段。
type SendMailTestToSelfRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	ClientRequestID  string `json:"clientRequestId,omitempty"` // 防双击幂等
}

// MailTestSendResult 是本人测试发信结果；Accepted 只表示 SMTP 接受，不是收信核实。
type MailTestSendResult struct {
	Status        string `json:"status"` // accepted|failed|unknown
	ShortID       string `json:"shortId,omitempty"`
	SubmissionID  string `json:"submissionId,omitempty"`
	AcceptedAt    string `json:"acceptedAt,omitempty"`
	Message       string `json:"message"`
	ErrorCategory string `json:"errorCategory,omitempty"`
}

// ConfirmMailTestReceiptRequest 记录本人在网页核实收到测试信。
type ConfirmMailTestReceiptRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	ShortID          string `json:"shortId"`
}

// OpenMailOfficialURLRequest 只打开固定官方 URL，不接受任意地址。
type OpenMailOfficialURLRequest struct {
	Page string `json:"page"` // webmail_standard|webmail_xc|help_security_password
}
