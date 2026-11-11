package desktop

import (
	"context"
	"errors"
	"time"

	kc "github.com/wt68/runcode/internal/knowledgechat"
	"github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

// runKnowledgeAnswer 完成宿主自己的单轮问答，不创建 engine.Session。
// 接受记录已在返回 receipt 前落盘；本函数只推进该条记录，失败也不回滚原问。
func (a *App) runKnowledgeAnswer(ctx context.Context, e *sessionEntry, run *knowledgeRun, accepted kc.Acceptance) {
	defer close(run.done)
	defer run.cancel()
	ctx = withKnowledgeRefreshBudget(ctx)
	a.emitKnowledgeChat(e, run, kc.Accepted)
	err := a.updateKnowledgeQuestion(ctx, e, run, func(q *kc.Question) { q.Status = kc.Retrieving })
	prepared := kc.Prepared{}
	answer := kc.Answer{}
	sent := false
	if err == nil {
		a.emitKnowledgeChat(e, run, kc.Retrieving)
		prepared, err = kc.Prepare(ctx, e.chat.scope, kc.RequestConfig{Model: accepted.Document.Model, Window: accepted.Document.Window, Output: accepted.Document.Output}, accepted.Question, accepted.Document.Questions, a.knowledgeChatDependencies(e))
	}
	if err == nil {
		phase := kc.Ready
		if prepared.Status == "empty" {
			phase = kc.Empty
		}
		a.emitKnowledgeChat(e, run, phase)
		answer, err = a.streamKnowledgeAnswer(ctx, e, run, prepared, accepted.Question.Selection.LibraryIDs, &sent)
	}
	phase, message := kc.Completed, ""
	if err != nil {
		phase, message = kc.Failed, "知识库检索或授权失败，本次未完成回答"
		if sent {
			message = kc.ErrAnswer.Error()
		}
		// 固定公开错误不包含上游响应正文；保留能指导用户操作的本地错误类别。
		for _, known := range []error{kc.ErrBudget, kc.ErrLimit, kc.ErrScope, kc.ErrAnswer, kc.ErrToolUse, kc.ErrOutputLimit, errKnowledgeAuthentication, errKnowledgeForbidden} {
			if errors.Is(err, known) {
				message = known.Error()
				break
			}
		}
	}
	if ctx.Err() != nil {
		phase, message = kc.Cancelled, "知识库问答已停止"
	}
	// 完成事件必须晚于终态原子保存。取消时仍保存已收集的部分文字，且不会
	// 把 failed/cancelled 回答加入下一轮模型历史（BuildRequest 只取 completed）。
	saveErr := a.updateKnowledgeQuestion(context.Background(), e, run, func(q *kc.Question) {
		q.Status, q.Error, q.Answer, q.Truncated = phase, message, answer.Text, answer.Truncated
		if sent {
			q.Sources = prepared.Sources
			q.AffectedLibraryIDs = prepared.AffectedLibraryIDs
		}
	})
	if saveErr != nil {
		phase, message = kc.Failed, "知识库问答状态保存失败，请重新打开会话修复"
	}
	event := protocol.KnowledgeChatEvent{Phase: phase, Error: message, Text: answer.Text, Truncated: answer.Truncated, InputTokens: answer.InputTokens, OutputTokens: answer.OutputTokens}
	if sent {
		event.Sources = knowledgeSources(prepared.Sources)
	}
	a.emitKnowledgeChatEvent(e, run, event)
	a.mu.Lock()
	if e.chat.run == run {
		e.chat.run = nil
		e.turnActive = false
	}
	a.mu.Unlock()
}

func (a *App) updateKnowledgeQuestion(ctx context.Context, e *sessionEntry, run *knowledgeRun, change func(*kc.Question)) error {
	return e.chat.store.Update(ctx, e.id, e.chat.scope, func(d *kc.Document) error {
		for i := range d.Questions {
			if d.Questions[i].ID == run.question {
				change(&d.Questions[i])
				return nil
			}
		}
		return kc.ErrFormat
	})
}

func knowledgeSources(sources []kc.Source) []protocol.KnowledgeSource {
	out := make([]protocol.KnowledgeSource, 0, len(sources))
	for _, s := range sources {
		out = append(out, protocol.KnowledgeSource{ID: s.ID, LibraryID: s.LibraryID, DocumentID: s.DocumentID, ChunkID: s.ChunkID, Title: s.Title, SourceURL: s.SourceURL})
	}
	return out
}

func (a *App) streamKnowledgeAnswer(ctx context.Context, e *sessionEntry, run *knowledgeRun, p kc.Prepared, selected []string, sent *bool) (kc.Answer, error) {
	// TokenSource 也捕获同一个模型期限，令牌刷新不能把回答等待无限延长。
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cfg := e.connectionConfig
	// Provider 内建重试和 401 回调都关闭，由宿主统一控制次数与重新授权。
	cfg.MaxRetries = -1
	cfg.OnUnauthorized = nil
	cfg.TokenSource = func() (string, error) {
		if err := a.checkKnowledgeChat(ctx, e); err != nil {
			return "", err
		}
		token, err := a.knowledgeToken(ctx, e, false)
		if err != nil {
			return "", err
		}
		if err := a.checkKnowledgeChat(ctx, e); err != nil {
			return "", err
		}
		return token, nil
	}
	provider, err := engine.BuildProvider(cfg)
	if err != nil {
		return kc.Answer{}, kc.ErrAnswer
	}
	return kc.Run(ctx, p.Request, kc.RunnerDependencies{
		Check: func(ctx context.Context) error { return a.checkKnowledgeChat(ctx, e) },
		Open: func(ctx context.Context, req llm.Request) (llm.Stream, error) {
			if err := a.checkKnowledgeChat(ctx, e); err != nil {
				return nil, err
			}
			if err := a.updateKnowledgeQuestion(ctx, e, run, func(q *kc.Question) { q.Status = kc.Generating }); err != nil {
				return nil, err
			}
			// 审计仅测试构建显式启用；此处才是宿主请求实际交给 Provider 的边界。
			if IsTestBuild() {
				a.audit.observer(e.id)("knowledge-answer", run.question, req)
			}
			*sent = true
			a.emitKnowledgeChatEvent(e, run, protocol.KnowledgeChatEvent{Phase: kc.Generating, Sources: knowledgeSources(p.Sources)})
			return provider.Stream(ctx, req)
		},
		Refresh: func(ctx context.Context) error {
			if _, err := a.knowledgeToken(ctx, e, true); err != nil {
				return err
			}
			// 复用已检索且已裁剪的不可变请求，但刷新后再次确认真正使用的依赖库，
			// 不重复成功的 SearchMore，也不把已撤权的历史内容继续送往模型。
			deps := a.knowledgeChatDependencies(e)
			// 零命中库也属于用户本轮的授权选择，不能因为未产生来源就漏掉撤权检查。
			ids := append(append([]string{}, p.AffectedLibraryIDs...), selected...)
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] {
					continue
				}
				seen[id] = true
				if err := deps.AuthorizeLibrary(ctx, e.chat.scope, id); err != nil {
					return err
				}
			}
			if err := deps.AuthorizeModel(ctx, e.chat.scope, p.Request.Model); err != nil {
				return err
			}
			return a.checkKnowledgeChat(ctx, e)
		},
		Delta: func(text, thinking string) {
			a.emitKnowledgeChatEvent(e, run, protocol.KnowledgeChatEvent{Phase: "delta", Text: text, Thinking: thinking})
		},
	})
}
