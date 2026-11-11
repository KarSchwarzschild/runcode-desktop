package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	kc "github.com/wt68/runcode/internal/knowledgechat"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
)

// 独立命名空间即使未打开或版本未知也不能回落到引擎恢复。
func isKnowledgeChatID(id string) bool { return strings.HasPrefix(strings.TrimSpace(id), "kc-") }
func sessionKind(e *sessionEntry) string {
	if e.chat != nil {
		return kc.Kind
	}
	return "agent"
}

// 本地已保存问答只核对稳定账号；离线或原库撤权不撤回本地历史。
// 这不是生成权限：发送、检索和模型请求仍须通过 checkKnowledgeChat 的完整边界检查。
func (a *App) checkKnowledgeHistoryAccess(e *sessionEntry) error {
	if e.chat == nil {
		return kc.ErrFormat
	}
	if err := a.checkKnowledgeEntry(e); err != nil {
		return err
	}
	return a.checkKnowledgeSavedAccount(e.chat.scope)
}

func (a *App) checkKnowledgeSavedAccount(scope kc.Scope) error {
	account, _ := a.knowledgeIdentity()
	if account == "" || account == "anonymous" || account != scope.Account {
		return kc.ErrScope
	}
	return nil
}

func (a *App) resumeKnowledgeChat(id string) (ResumedSession, error) {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	if e, err := a.entryOf(id); err == nil && e.chat != nil {
		return a.knowledgeChatResumed(e)
	}
	a.mu.Lock()
	ws := a.workspace
	a.mu.Unlock()
	store := kc.NewStore(ws)
	d, err := store.Load(context.Background(), id)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	if err := a.checkKnowledgeSavedAccount(d.Scope); err != nil {
		return ResumedSession{}, wireError(err)
	}
	interrupted := false
	for _, q := range d.Questions {
		interrupted = interrupted || !kc.Terminal(q.Status)
	}
	if interrupted {
		// 只落盘中断标记，不重试检索、不访问模型，避免恢复操作产生新执行。
		err = store.Update(context.Background(), id, d.Scope, func(doc *kc.Document) error {
			for i := range doc.Questions {
				if !kc.Terminal(doc.Questions[i].Status) {
					doc.Questions[i].Status = kc.Interrupted
					doc.Questions[i].Error = "上次运行已中断，请显式重试"
				}
			}
			return nil
		})
		if err != nil {
			return ResumedSession{}, wireError(err)
		}
	}
	cfg := engine.Config{CWD: ws, Model: d.Model, Provider: "openai", BaseURL: d.Scope.ModelBridge + tenantPathPrefix(d.Scope.Tenant) + "/v1", MaxTokens: d.Output, MaxContextTokens: d.Window}
	if _, err := a.registerKnowledgeChat(d, cfg, store); err != nil {
		return ResumedSession{}, err
	}
	e, err := a.entryOf(id)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	return a.knowledgeChatResumed(e)
}

// 原题选库与实际入模来源分别来自冻结快照，不能用当前标签或重新查到的目录替代。
func knowledgeQuestionView(q kc.Question) *QuestionKnowledge {
	// Applied only when a completed turn actually used retrieved sources (task 24).
	v := &QuestionKnowledge{Status: q.Status, Error: q.Error, Applied: q.Status == kc.Completed && len(q.Sources) > 0, Truncated: q.Truncated, Libraries: []KnowledgeBaseSummary{}, Sources: []KnowledgeSource{}}
	for _, id := range q.Selection.LibraryIDs {
		v.Libraries = append(v.Libraries, KnowledgeBaseSummary{ID: id, Title: id})
	}
	for _, src := range q.Sources {
		v.Sources = append(v.Sources, KnowledgeSource{ID: src.ID, LibraryID: src.LibraryID, DocumentID: src.DocumentID, ChunkID: src.ChunkID, Title: src.Title, SourceURL: src.SourceURL})
	}
	return v
}

func (a *App) knowledgeChatResumed(e *sessionEntry) (ResumedSession, error) {
	info, err := a.knowledgeChatInfo(e)
	if err != nil {
		return ResumedSession{}, err
	}
	d, err := e.chat.store.Load(context.Background(), e.id)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	if d.Scope != e.chat.scope {
		return ResumedSession{}, wireError(kc.ErrScope)
	}
	blocks := []ResumedBlock{}
	for _, q := range d.Questions {
		blocks = append(blocks, ResumedBlock{Kind: "user", QuestionID: q.ID, Text: q.Text, Knowledge: knowledgeQuestionView(q)})
		if q.Answer != "" {
			blocks = append(blocks, ResumedBlock{Kind: "assistant", Text: q.Answer})
		}
	}
	if err := a.checkKnowledgeHistoryAccess(e); err != nil {
		return ResumedSession{}, wireError(err)
	}
	var source *QuestionReference
	if d.Parent != nil {
		source = &QuestionReference{SessionID: d.Parent.SessionID, QuestionID: d.Parent.QuestionID}
	}
	return ResumedSession{Info: info, Blocks: blocks, Source: source}, nil
}

func (a *App) deleteKnowledgeChat(id string) error {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	// 必须在关闭前捕获所属工作区，关闭会自动把全局焦点切到另一工作区。
	store, d, err := a.savedKnowledgeChat(id)
	if err != nil {
		return wireError(err)
	}
	if e, err := a.entryOf(id); err == nil && e.chat != nil {
		ctx, cancel := context.WithTimeout(context.Background(), sessionCloseTimeout)
		defer cancel()
		if err := a.closeEntryHeld(ctx, e, true); err != nil {
			return wireError(err)
		}
	}
	return wireError(store.Delete(context.Background(), id, d.Scope))
}

func (a *App) knowledgeChatSummaries(ws string) ([]SessionSummary, error) {
	if ws == "" {
		return nil, nil
	}
	docs, err := kc.NewStore(ws).List(context.Background())
	if err != nil {
		return nil, err
	}
	out := []SessionSummary{}
	for _, d := range docs {
		if a.checkKnowledgeSavedAccount(d.Scope) != nil {
			continue
		}
		title := d.Title
		if title == "" {
			title = "知识库问答"
		}
		out = append(out, SessionSummary{Kind: kc.Kind, ID: d.ID, Title: title, Turns: len(d.Questions)})
	}
	return out, nil
}

func (a *App) getKnowledgeChatQuestion(ref QuestionReference) (QuestionDraft, error) {
	e, err := a.entryOf(ref.SessionID)
	if err != nil {
		return QuestionDraft{}, wireError(err)
	}
	if err := a.checkKnowledgeHistoryAccess(e); err != nil {
		return QuestionDraft{}, wireError(err)
	}
	d, err := e.chat.store.Load(context.Background(), e.id)
	if err != nil {
		return QuestionDraft{}, wireError(err)
	}
	if d.Scope != e.chat.scope {
		return QuestionDraft{}, wireError(kc.ErrScope)
	}
	for _, q := range d.Questions {
		if q.ID == ref.QuestionID {
			if err := a.checkKnowledgeHistoryAccess(e); err != nil {
				return QuestionDraft{}, wireError(err)
			}
			return QuestionDraft{Source: ref, Text: q.Text, Images: []QuestionImage{}, Knowledge: knowledgeQuestionView(q)}, nil
		}
	}
	return QuestionDraft{}, wireError(errors.New("提问不存在"))
}

func (a *App) forkKnowledgeChat(ref QuestionReference) (ResumedSession, error) {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	e, err := a.entryOf(ref.SessionID)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	if !e.questionMu.TryLock() {
		return ResumedSession{}, wireError(kc.ErrBusy)
	}
	defer e.questionMu.Unlock()
	if err := a.checkKnowledgeHistoryAccess(e); err != nil {
		return ResumedSession{}, wireError(err)
	}
	d, err := e.chat.store.Fork(context.Background(), e.id, e.chat.scope, ref.QuestionID)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	a.mu.Lock()
	focused := a.focused
	a.mu.Unlock()
	_, registerErr := a.registerKnowledgeChat(d, e.connectionConfig, e.chat.store)
	// 分支准备不能改变原编辑草稿所属的会话；前端提交成功后再显式切换。
	a.mu.Lock()
	if prior := a.entryLocked(focused); prior != nil && !prior.closed {
		a.focusLocked(prior)
	}
	a.mu.Unlock()
	if registerErr != nil {
		return ResumedSession{}, registerErr
	}
	child, err := a.entryOf(d.ID)
	if err != nil {
		return ResumedSession{}, wireError(err)
	}
	return a.knowledgeChatResumed(child)
}

// 调用方持有 questionMu；只核对存储血缘，不检索，不创建 receipt。
func (a *App) knowledgeChatSubmitInput(ctx context.Context, e *sessionEntry, req SubmitQuestionRequest) (kc.SubmitInput, error) {
	if req.KnowledgeRevision == nil {
		return kc.SubmitInput{}, kc.ErrRevision
	}
	in := kc.SubmitInput{SubmissionID: req.SubmissionID, Text: req.Text, Revision: *req.KnowledgeRevision}
	if req.Source == nil {
		if req.KnowledgeOverride != nil {
			return kc.SubmitInput{}, kc.ErrScope
		}
		return in, nil
	}
	d, err := e.chat.store.Load(ctx, e.id)
	if err != nil {
		return kc.SubmitInput{}, err
	}
	parent := kc.Reference{SessionID: req.Source.SessionID, QuestionID: req.Source.QuestionID}
	if d.Scope != e.chat.scope || d.Parent == nil || *d.Parent != parent {
		return kc.SubmitInput{}, kc.ErrScope
	}
	source, err := e.chat.store.Load(ctx, parent.SessionID)
	if err != nil {
		return kc.SubmitInput{}, err
	}
	if source.Scope != d.Scope {
		return kc.SubmitInput{}, kc.ErrScope
	}
	found := false
	for _, q := range source.Questions {
		if q.ID == parent.QuestionID {
			found = true
		}
	}
	if !found {
		return kc.SubmitInput{}, kc.ErrScope
	}
	in.Parent = &parent
	if req.KnowledgeOverride != nil {
		ids := append([]string{}, req.KnowledgeOverride.LibraryIDs...)
		in.LibraryIDs = &ids
	}
	return in, nil
}

func (a *App) savedKnowledgeChat(id string) (*kc.Store, kc.Document, error) {
	if !isKnowledgeChatID(id) {
		return nil, kc.Document{}, kc.ErrFormat
	}
	a.mu.Lock()
	ws := a.workspace
	var store *kc.Store
	if e := a.entryLocked(id); e != nil && e.chat != nil {
		ws = e.workspace
		// 复用 worker 的同一把存储锁，避免标题的读改写覆盖刚保存的终态回答。
		store = e.chat.store
	}
	a.mu.Unlock()
	if store == nil {
		store = kc.NewStore(ws)
	}
	d, err := store.Load(context.Background(), id)
	if err != nil {
		return nil, kc.Document{}, err
	}
	if err := a.checkKnowledgeSavedAccount(d.Scope); err != nil {
		return nil, kc.Document{}, err
	}
	return store, d, nil
}

// RenameKnowledgeChat 只修改宿主记录，不调用标题模型或普通引擎。
func (a *App) RenameKnowledgeChat(sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 200 {
		return wireError(errors.New("标题须为 1 到 200 个字符"))
	}
	a.startMu.Lock()
	defer a.startMu.Unlock()
	store, d, err := a.savedKnowledgeChat(strings.TrimSpace(sessionID))
	if err != nil {
		return wireError(err)
	}
	return wireError(store.Update(context.Background(), d.ID, d.Scope, func(doc *kc.Document) error { doc.Title = title; return nil }))
}

// ExportKnowledgeChat 返回纯文本问答和来源元数据；不导出凭据或检索片段。
func (a *App) ExportKnowledgeChat(sessionID string) (string, error) {
	_, d, err := a.savedKnowledgeChat(strings.TrimSpace(sessionID))
	if err != nil {
		return "", wireError(err)
	}
	var out strings.Builder
	if d.Title != "" {
		fmt.Fprintf(&out, "%s\n\n", d.Title)
	}
	for _, q := range d.Questions {
		fmt.Fprintf(&out, "提问：%s\n\n回答：%s\n状态：%s\n", q.Text, q.Answer, q.Status)
		if q.Truncated {
			out.WriteString("回答达到输出上限，内容已截断。\n")
		}
		for _, src := range q.Sources {
			fmt.Fprintf(&out, "来源 [%s]：%s %s\n", src.ID, src.Title, src.SourceURL)
		}
		out.WriteString("\n")
	}
	if err := a.checkKnowledgeSavedAccount(d.Scope); err != nil {
		return "", wireError(err)
	}
	return out.String(), nil
}
