// Package knowledgechat 独立管理知识库问答，不使用引擎会话日志或工具循环。
// 检索正文只在请求内存中存在；持久记录仅包含原问、回答、库标识及来源元数据。
package knowledgechat

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Kind 标识只能由宿主管理的知识库会话，不能回落到引擎工具循环。
const Kind = "knowledge"

// 问题状态覆盖持久准入、准备、生成和终态；恢复只把未完成状态变为 Interrupted。
const (
	Accepted    = "accepted"
	Retrieving  = "retrieving"
	Ready       = "ready"
	Empty       = "empty"
	Generating  = "generating"
	Completed   = "completed"
	Failed      = "failed"
	Cancelled   = "cancelled"
	Interrupted = "interrupted"
)

var (
	// ErrFormat 表示损坏或未知版本，拒绝当作普通历史继续执行。
	ErrFormat = errors.New("知识库会话记录损坏或版本不受支持")
	// ErrScope 表示账号、服务或父题边界不匹配。
	ErrScope = errors.New("知识库会话账号或服务边界已变化，请新建会话")
	// ErrBusy 阻止同一会话重叠接受新问题。
	ErrBusy = errors.New("知识库会话正在处理上一条问题")
	// ErrConflict 阻止使用同一 submissionId 改写已接受输入。
	ErrConflict = errors.New("同一提交编号的内容不一致")
	// ErrRevision 要求提交使用当前确认的选库版本。
	ErrRevision = errors.New("知识库选择版本已变化，请刷新后重试")
	// ErrInput 表示问题原文或选库标识不符合上限与格式。
	ErrInput = errors.New("知识库问答仅支持最多 4000 字符的非空文字和最多 5 个知识库")
	// ErrLimit 表示文件大小或累计库依赖达到上限。
	ErrLimit = errors.New("知识库会话达到存储或累计 50 个库上限，请新建会话")
	// ErrBudget 表示已确认的模型窗口无法容纳本次输入。
	ErrBudget = errors.New("无法在已确认的模型上下文窗口内放入问题和参考资料")
	// ErrProtocol 表示上游检索结果无法安全使用。
	ErrProtocol = errors.New("知识库服务返回了无效的检索结果")
)

// Scope 固定会话的账号、租户、工作区和两个服务边界，不保存令牌。
type Scope struct {
	Workspace       string `json:"workspace"`
	Account         string `json:"account"`
	Tenant          string `json:"tenant"`
	KnowledgeBridge string `json:"knowledgeBridge"`
	ModelBridge     string `json:"modelBridge"`
}

// Selection 是一次确认后的选库版本；Question 保存自己的副本，不随标签变化。
type Selection struct {
	Revision   int64    `json:"revision"`
	LibraryIDs []string `json:"libraryIds"`
}

// Source 是实际入模片段的展示元数据，不包含片段正文。
type Source struct {
	ID         string `json:"id"`
	LibraryID  string `json:"libraryId"`
	DocumentID string `json:"documentId"`
	ChunkID    string `json:"chunkId"`
	Title      string `json:"title"`
	SourceURL  string `json:"sourceUrl,omitempty"`
}

// Reference 只记录已核对的父会话与原题，不承载检索正文。
type Reference struct {
	SessionID  string `json:"sessionId"`
	QuestionID string `json:"questionId"`
}

// Question 保存原文和不可变选择；Answer 仅在终态写入成功后作为历史使用。
type Question struct {
	ID           string     `json:"id"`
	SubmissionID string     `json:"submissionId"`
	Text         string     `json:"text"`
	Selection    Selection  `json:"selection"`
	Parent       *Reference `json:"parent,omitempty"`
	// SelectionOverridden 区分同一提交的 nil 覆盖与显式空覆盖，保障幂等比较。
	SelectionOverridden bool     `json:"selectionOverridden,omitempty"`
	Status              string   `json:"status"`
	Answer              string   `json:"answer,omitempty"`
	Sources             []Source `json:"sources,omitempty"`
	AffectedLibraryIDs  []string `json:"affectedLibraryIds,omitempty"`
	Error               string   `json:"error,omitempty"`
	Truncated           bool     `json:"truncated,omitempty"`
}

// Document 是原子替换的宿主会话文档；Parent 保留重试分支的可信父题关联。
type Document struct {
	Version   int        `json:"version"`
	Kind      string     `json:"kind"`
	ID        string     `json:"id"`
	Scope     Scope      `json:"scope"`
	Model     string     `json:"model"`
	Window    int        `json:"window"`
	Output    int        `json:"output"`
	Title     string     `json:"title,omitempty"`
	Selection Selection  `json:"selection"`
	Parent    *Reference `json:"parent,omitempty"`
	Questions []Question `json:"questions"`
}

// SubmitInput 是经过桌面入口校验的准入输入；存储仍会复核父题和选择边界。
type SubmitInput struct {
	SubmissionID, Text string
	Revision           int64
	Parent             *Reference
	// nil 沿用当前选库；指向空切片表示重试时明确清空，不能混为未传。
	LibraryIDs *[]string
}

// Acceptance 仅在原问与快照原子落盘后返回，供 worker 使用同一不可变输入。
type Acceptance struct {
	Document Document
	Question Question
}

// Terminal 标识不会在重开时自动运行的终态。
func Terminal(status string) bool {
	return status == Completed || status == Failed || status == Cancelled || status == Interrupted
}

// ValidLibrary 只接受非零、规范小写的 UUID 库标识。
func ValidLibrary(id string) bool {
	value, err := uuid.Parse(id)
	return err == nil && value != uuid.Nil && value.String() == id
}
func validIDs(ids []string, limit int) bool {
	if len(ids) > limit {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !ValidLibrary(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func validText(s string) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 4000
}
func validID(id string) bool {
	return strings.HasPrefix(id, "kc-") && len(id) > 3 && len(id) <= 100 && !strings.ContainsAny(id, "/\\:\x00") && strings.TrimSpace(id) == id
}
