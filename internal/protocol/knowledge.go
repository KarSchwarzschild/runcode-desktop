package protocol

// KnowledgeBaseSummary describes a catalog entry without document contents.
type KnowledgeBaseSummary struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	Path               string `json:"path"`
	Source             string `json:"source"`
	DocumentCount      int    `json:"documentCount"`
	DocumentCountKnown bool   `json:"documentCountKnown"`
	DirectoryURL       string `json:"directoryUrl,omitempty"`
	UnavailableReason  string `json:"unavailableReason,omitempty"`
	// FailureKind 仅用于详情失败占位：not_found／forbidden／authentication／transient 等；成功摘要不带。
	FailureKind string `json:"failureKind,omitempty"`
}

// KnowledgeListRequest explicitly addresses the session's catalog.
type KnowledgeListRequest struct {
	SessionID string `json:"sessionId"`
	Query     string `json:"query"`
	Page      int    `json:"page"`
	PageSize  int    `json:"pageSize"`
}

// KnowledgePage distinguishes real, explicitly enabled demo, and unavailable catalogs.
type KnowledgePage struct {
	Items             []KnowledgeBaseSummary `json:"items"`
	Page              int                    `json:"page"`
	PageSize          int                    `json:"pageSize"`
	Total             int                    `json:"total"`
	HasMore           bool                   `json:"hasMore"`
	Mode              string                 `json:"mode"`
	UnavailableReason string                 `json:"unavailableReason"`
	MaxSelection      int                    `json:"maxSelection"`
	// FailureKind：authentication／forbidden／context_changed／transient／configuration；缺省兼容旧端。
	FailureKind string `json:"failureKind,omitempty"`
}

// SessionKnowledgeView is the last durably confirmed selection.
type SessionKnowledgeView struct {
	SessionID         string                 `json:"sessionId"`
	Revision          int64                  `json:"revision"`
	Libraries         []KnowledgeBaseSummary `json:"libraries"`
	Mode              string                 `json:"mode"`
	UnavailableReason string                 `json:"unavailableReason"`
}

// SetSessionKnowledgeRequest replaces the entire selection using a revision check.
type SetSessionKnowledgeRequest struct {
	SessionID        string   `json:"sessionId"`
	LibraryIDs       []string `json:"libraryIds"`
	ExpectedRevision int64    `json:"expectedRevision"`
}

// KnowledgeSource exposes provenance only, never retrieved document text.
type KnowledgeSource struct {
	ID         string `json:"id"`
	LibraryID  string `json:"libraryId"`
	DocumentID string `json:"documentId"`
	ChunkID    string `json:"chunkId"`
	Title      string `json:"title"`
	SourceURL  string `json:"sourceUrl,omitempty"`
}

// QuestionKnowledge describes frozen selection and tool/search status for one question.
// Status values for the agent tool path: not_used, searching, matched, empty, failed, cancelled
// (legacy knowledge-chat statuses such as retrieving/completed remain valid for old records).
// Applied is true only when tool evidence was actually used — never merely because libraries were selected.
type QuestionKnowledge struct {
	Libraries []KnowledgeBaseSummary `json:"libraries"`
	Status    string                 `json:"status"`
	Applied   bool                   `json:"applied"`
	Sources   []KnowledgeSource      `json:"sources"`
	Error     string                 `json:"error,omitempty"`
	Truncated bool                   `json:"truncated,omitempty"`
}

// KnowledgeContextStatus updates one accepted question within its session envelope.
type KnowledgeContextStatus struct {
	QuestionID string            `json:"questionId"`
	Knowledge  QuestionKnowledge `json:"knowledge"`
}

