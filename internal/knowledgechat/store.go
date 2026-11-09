package knowledgechat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxDocumentBytes = 50 << 20

// Store 在单个工作区内原子保存宿主文档。已打开的会话必须共享此实例，
// 让提交、终态保存和重命名使用同一把锁，避免读改写互相覆盖。
type Store struct {
	workspace    string
	mu           sync.Mutex
	beforeRename func() error
}

// NewStore 把所有文件操作限制在指定工作区的独立知识库目录内。
func NewStore(workspace string) *Store { return &Store{workspace: workspace} }
func documentPath(id string) (string, error) {
	if !validID(id) {
		return "", ErrFormat
	}
	return filepath.Join(".runcode", "knowledge-chat", id+".json"), nil
}

func validateDocument(d Document) error {
	if d.Version != 1 || d.Kind != Kind || !validID(d.ID) || d.Scope.Workspace == "" || d.Scope.Account == "" || d.Scope.Account == "anonymous" || d.Scope.Tenant == "" || d.Scope.KnowledgeBridge == "" || d.Scope.ModelBridge == "" || d.Model == "" || d.Selection.Revision < 0 || !validIDs(d.Selection.LibraryIDs, 5) {
		return ErrFormat
	}
	if !validReference(d.Parent) {
		return ErrFormat
	}
	ids, submissions := map[string]bool{}, map[string]bool{}
	for _, q := range d.Questions {
		if !validReference(q.Parent) || q.SelectionOverridden && q.Parent == nil || !utf8.ValidString(q.Answer) || len(q.Answer) > 64<<10 {
			return ErrFormat
		}
		if q.ID == "" || ids[q.ID] || q.SubmissionID == "" || submissions[q.SubmissionID] || !validText(q.Text) || q.Selection.Revision < 0 || !validIDs(q.Selection.LibraryIDs, 5) || !validIDs(q.AffectedLibraryIDs, 50) {
			return ErrFormat
		}
		ids[q.ID], submissions[q.SubmissionID] = true, true
		// 终态来源必须属于已经记录的传递性授权集合，损坏文件不能借恢复
		// 绕过累计库限制，也不能声称使用了未被授权历史追踪的资料。
		if len(q.Sources) > 5 {
			return ErrFormat
		}
		sources := map[string]bool{}
		for _, source := range q.Sources {
			if source.ID == "" || sources[source.ID] || !ValidLibrary(source.LibraryID) || source.DocumentID == "" || source.ChunkID == "" || !slices.Contains(q.AffectedLibraryIDs, source.LibraryID) {
				return ErrFormat
			}
			sources[source.ID] = true
		}
		switch q.Status {
		case Accepted, Retrieving, Ready, Empty, Generating, Completed, Failed, Cancelled, Interrupted:
		default:
			return ErrFormat
		}
	}
	return CheckLibraryLimit(d, d.Selection.LibraryIDs)
}
func (s *Store) load(ctx context.Context, id string) (Document, error) {
	var d Document
	if err := ctx.Err(); err != nil {
		return d, err
	}
	path, err := documentPath(id)
	if err != nil {
		return d, err
	}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return d, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(path)
	if err != nil {
		return d, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1))
	if err != nil {
		return d, err
	}
	if len(raw) > maxDocumentBytes {
		return d, ErrLimit
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &d) != nil || d.ID != id || d.Scope.Workspace != s.workspace {
		return Document{}, ErrFormat
	}
	if err := validateDocument(d); err != nil {
		return Document{}, err
	}
	return d, ctx.Err()
}

// Load 校验原始文档格式与工作区，不执行恢复任务或访问网络。
func (s *Store) Load(ctx context.Context, id string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx, id)
}
func (s *Store) save(ctx context.Context, d Document) error {
	if err := validateDocument(d); err != nil {
		return err
	}
	if d.Scope.Workspace != s.workspace {
		return ErrScope
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(raw) > maxDocumentBytes {
		return ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	path, _ := documentPath(d.ID)
	if err := root.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + "." + uuid.NewString() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return root.Rename(tmp, path)
}

// Create 只创建新会话；损坏旧文件也不能被静默覆盖。
func (s *Store) Create(ctx context.Context, d Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.load(ctx, d.ID); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return ErrConflict
	}
	return s.save(ctx, d)
}

// Update 在同一把锁内校验边界、修改与原子保存，不允许回调更改身份或会话 ID。
func (s *Store) Update(ctx context.Context, id string, scope Scope, change func(*Document) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if d.Scope != scope {
		return ErrScope
	}
	if err := change(&d); err != nil {
		return err
	}
	if d.Scope != scope || d.ID != id {
		return ErrScope
	}
	return s.save(ctx, d)
}

// Accept 原子保存原问、选库快照和 accepted；返回的 bool 为是否重放已有 receipt。
// 任何写入失败都不产生 receipt，也不会启动检索或模型请求。
func (s *Store) Accept(ctx context.Context, id string, scope Scope, in SubmitInput) (Acceptance, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load(ctx, id)
	if err != nil {
		return Acceptance{}, false, err
	}
	if d.Scope != scope {
		return Acceptance{}, false, ErrScope
	}
	if in.SubmissionID == "" || len(in.SubmissionID) > 128 || !validText(in.Text) {
		return Acceptance{}, false, ErrInput
	}
	// 逐题覆盖只允许已验证的重试分支，不能把任意来源伪装成当前会话的父题。
	if in.Parent != nil && !sameReference(in.Parent, d.Parent) || in.LibraryIDs != nil && in.Parent == nil {
		return Acceptance{}, false, ErrScope
	}
	selected := d.Selection.LibraryIDs
	if in.LibraryIDs != nil {
		selected = *in.LibraryIDs
	}
	if !validIDs(selected, 5) {
		return Acceptance{}, false, ErrInput
	}
	for _, q := range d.Questions {
		if q.SubmissionID == in.SubmissionID {
			// 重复 IPC 应返回最初 receipt。之后改标签不改变原输入，但必须区分
			// “沿用选择”与“显式覆盖为空”，并核对覆盖的具体内容和父题。
			if q.Text != in.Text || q.Selection.Revision != in.Revision || !sameReference(q.Parent, in.Parent) || q.SelectionOverridden != (in.LibraryIDs != nil) || in.LibraryIDs != nil && !slices.Equal(q.Selection.LibraryIDs, *in.LibraryIDs) {
				return Acceptance{}, false, ErrConflict
			}
			return Acceptance{d, q}, true, nil
		}
	}
	for _, q := range d.Questions {
		if !Terminal(q.Status) {
			return Acceptance{}, false, ErrBusy
		}
	}
	if d.Selection.Revision != in.Revision {
		return Acceptance{}, false, ErrRevision
	}
	if err := CheckLibraryLimit(d, selected); err != nil {
		return Acceptance{}, false, err
	}
	q := Question{ID: uuid.NewString(), SubmissionID: in.SubmissionID, Text: in.Text, Selection: Selection{d.Selection.Revision, append([]string{}, selected...)}, Status: Accepted}
	q.SelectionOverridden = in.LibraryIDs != nil
	if in.Parent != nil {
		parent := *in.Parent
		q.Parent = &parent
	}
	d.Questions = append(d.Questions, q)
	if err := s.save(ctx, d); err != nil {
		return Acceptance{}, false, err
	}
	return Acceptance{d, q}, false, nil
}

// CheckLibraryLimit 保留所有已保存回答的传递性库依赖，移除标签不能抹去历史授权范围。
func CheckLibraryLimit(d Document, selected []string) error {
	ids := map[string]bool{}
	for _, id := range selected {
		ids[id] = true
	}
	for _, q := range d.Questions {
		for _, id := range q.AffectedLibraryIDs {
			ids[id] = true
		}
	}
	if len(ids) > 50 {
		return ErrLimit
	}
	return nil
}

// List 只列举宿主专用目录，并逐条校验；不读取或迁移普通引擎日志。
func (s *Store) List(ctx context.Context) ([]Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(filepath.Join(".runcode", "knowledge-chat"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	out := []Document{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		d, err := s.load(ctx, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Delete 再次核对保存边界后只移除目标宿主文件，不扫描或删除其他会话。
func (s *Store) Delete(ctx context.Context, id string, scope Scope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if d.Scope != scope {
		return ErrScope
	}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	path, _ := documentPath(id)
	return root.Remove(path)
}
