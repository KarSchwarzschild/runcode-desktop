// Package mail 拥有本机邮箱领域类型、草稿修订与一次性投递规则。
// 本包不得依赖 desktop、automation、localusage 或 Wails。
package mail

import "time"

// 投递 / 草稿状态（方案 §5）。draft_ready 是工具结果语义，不是库内状态。
const (
	StatusDraft     = "draft"
	StatusScheduled = "scheduled"
	StatusQueued    = "queued"
	StatusSending   = "sending"
	StatusAccepted  = "accepted"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"
	StatusMissed    = "missed"
	StatusCancelled = "cancelled"
)

// 发送模式。
const (
	ModeTogether    = "together"
	ModeIndividual  = "individual"
	ModeImmediate   = "immediate"
	ModeScheduledAt = "scheduled_at"
)

// LateWindow 是计划时刻之后仍可自动补发的闭区间上限。
const LateWindow = 10 * time.Minute

// DailyQuotaDefault 是本机单账号每日默认额度（本应用）。
const DailyQuotaDefault = 300

// OwnerContext 是身份代际上下文；不含凭据。
type OwnerContext struct {
	ViewID string
	Epoch  int64
}

// AccountRef 是已保存账号的脱敏引用。
type AccountRef struct {
	AccountID string
	Address   string
	Display   string
}

// MailAddress 是已规范化的邮箱地址。
type MailAddress struct {
	Name    string
	Address string
}

// AttachmentRef 是来源会话内已授权附件的引用（字节在确认时快照）。
type AttachmentRef struct {
	SourceSessionID string
	RelPath         string
	Name            string
	ByteSize        int64
	ContentSHA256   string // 确认快照后填充；草稿阶段可空
}

// DraftSource 记录模型提案来源；用户编辑不覆盖这些字段。
type DraftSource struct {
	SessionID    string
	TurnID       string
	ToolUseID    string
	ProposalMD   string // 模型原始 Markdown，只读历史
	CreatedAtUTC time.Time
}

// Draft 是可编辑邮件草稿。
type Draft struct {
	ID             string
	OwnerScope     string
	AccountID      string
	Revision       int64
	Status         string
	BlockedReason  string
	Source         DraftSource
	Subject        string
	BodyMarkdown   string
	To             []MailAddress
	Cc             []MailAddress
	Bcc            []MailAddress
	Attachments    []AttachmentRef
	SendMode       string // together | individual
	DispatchMode   string // immediate | scheduled_at
	ScheduledLocal string // YYYY-MM-DDTHH:mm:ss（任务时区本地墙钟）
	TimeZone       string // IANA
	ScheduledAtUTC *time.Time
	CreatedAtUTC   time.Time
	UpdatedAtUTC   time.Time
}

// DraftEdit 是用户可保存的字段（不含 owner / source）。
type DraftEdit struct {
	AccountID      string
	Subject        string
	BodyMarkdown   string
	To             []MailAddress
	Cc             []MailAddress
	Bcc            []MailAddress
	Attachments    []AttachmentRef
	SendMode       string
	DispatchMode   string
	ScheduledLocal string
	TimeZone       string
}

// Contact / Group 通信录实体。
type Contact struct {
	ID           string
	OwnerScope   string
	Revision     int64
	Name         string
	Emails       []string
	CreatedAtUTC time.Time
	UpdatedAtUTC time.Time
}

type Group struct {
	ID           string
	OwnerScope   string
	Revision     int64
	Name         string
	MemberIDs    []string
	CreatedAtUTC time.Time
	UpdatedAtUTC time.Time
}

// PreparedSnapshot 是确认前冻结的定稿。
type PreparedSnapshot struct {
	ID             string
	DraftID        string
	DraftRevision  int64
	OwnerScope     string
	AccountID      string
	Subject        string
	BodyMarkdown   string
	ResolvedTo     []MailAddress
	ResolvedCc     []MailAddress
	ResolvedBcc    []MailAddress
	Attachments    []AttachmentRef
	SendMode       string
	DispatchMode   string
	ScheduledAtUTC *time.Time
	Digest         string
	CreatedAtUTC   time.Time
}

// Confirmation 是一次性本人确认令牌，绑定草稿修订与定稿摘要。
type Confirmation struct {
	ID            string
	OwnerScope    string
	DraftID       string
	DraftRevision int64
	SnapshotID    string
	Digest        string
	ExpiresAtUTC  time.Time
	UsedAtUTC     *time.Time
	CreatedAtUTC  time.Time
}

// Submission / Item / Attempt 记录投递意图与尝试。
type Submission struct {
	ID             string
	OwnerScope     string
	DraftID        string
	DraftRevision  int64
	SnapshotID     string
	ConfirmationID string
	Status         string
	BlockedReason  string
	CreatedAtUTC   time.Time
	UpdatedAtUTC   time.Time
}

type SubmissionItem struct {
	ID             string
	SubmissionID   string
	OwnerScope     string
	Recipient      MailAddress
	Status         string
	BlockedReason  string
	ScheduledAtUTC *time.Time
	ClaimedAtUTC   *time.Time
	FinishedAtUTC  *time.Time
	AttemptCount   int
}

type Attempt struct {
	ID            string
	ItemID        string
	OwnerScope    string
	Phase         string
	Outcome       string
	Detail        string
	StartedAtUTC  time.Time
	FinishedAtUTC time.Time
}

// QuotaReservation 是单账号日额度预占。
type QuotaReservation struct {
	ID           string
	OwnerScope   string
	AccountID    string
	DayKey       string // 账号时区 YYYY-MM-DD
	Count        int
	Kind         string // reserved | consumed | released
	ItemID       string
	CreatedAtUTC time.Time
}
