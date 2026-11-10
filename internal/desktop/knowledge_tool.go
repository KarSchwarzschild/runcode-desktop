package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/wt68/runcode/internal/knowledgetool"
	kc "github.com/wt68/runcode/internal/knowledgechat"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

// knowledgeToolHost binds knowledge_search Run to the session's Bridge retrieve.
type knowledgeToolHost struct {
	app       *App
	sessionID string
}

func (h knowledgeToolHost) Retrieve(ctx context.Context, query string, libraryIDs []string, meta knowledgetool.CallMeta) ([]knowledgetool.RawHit, string, error) {
	e, err := h.app.entryOf(h.sessionID)
	if err != nil {
		return nil, "", errors.New("会话不可用，无法检索知识库")
	}
	if meta.MessageID != "" && e.knowledgeTurn != nil {
		e.knowledgeTurn.bindQuestion(meta.MessageID)
	}
	if mode, reason := knowledgeMode(e); mode != "real" {
		if reason == "" {
			reason = "当前会话不能检索真实知识库"
		}
		return nil, "", errors.New(reason)
	}
	if err := h.app.checkKnowledgeAccount(e); err != nil {
		return nil, "", err
	}
	for _, id := range libraryIDs {
		if _, err := h.app.knowledgeDetail(ctx, e, id); err != nil {
			return nil, "", fmt.Errorf("知识库 %s 当前不可用或无权访问", id)
		}
	}
	if meta.MessageID != "" {
		h.app.emitKnowledgeSearching(e, meta.MessageID)
	}
	payload, _ := json.Marshal(struct {
		LibraryIDs []string `json:"libraryIds"`
		Question   string   `json:"question"`
	}{libraryIDs, query})
	var r struct {
		Status    string `json:"status"`
		Libraries []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"libraries"`
		Hits []struct {
			LibraryID  string `json:"libraryId"`
			DocumentID string `json:"documentId"`
			ChunkID    string `json:"chunkId"`
			Title      string `json:"title"`
			Text       string `json:"text"`
			SourceURL  string `json:"sourceUrl"`
		} `json:"hits"`
	}
	if err := h.app.knowledgeRequest(ctx, e, http.MethodPost, "/retrieve", payload, &r); err != nil {
		return nil, "", err
	}
	// Soft-validate Bridge envelope; knowledgetool still rejects foreign hits.
	kcHits := make([]kc.Hit, 0, len(r.Hits))
	for _, hit := range r.Hits {
		kcHits = append(kcHits, kc.Hit{
			LibraryID: hit.LibraryID, DocumentID: hit.DocumentID, ChunkID: hit.ChunkID,
			Title: hit.Title, Text: hit.Text,
		})
	}
	libs := make([]kc.Library, 0, len(r.Libraries))
	for _, lib := range r.Libraries {
		libs = append(libs, kc.Library{ID: lib.ID, Title: lib.Title})
	}
	if err := kc.ValidateRetrieved(libraryIDs, kc.Retrieved{Status: r.Status, Libraries: libs, Hits: kcHits}); err != nil {
		// Bridge may return a subset of authorized libraries; fall back to hit-level checks only.
		if r.Status != "matched" && r.Status != "empty" {
			return nil, "", errors.New("知识库服务返回了无效的检索结果")
		}
	}
	out := make([]knowledgetool.RawHit, 0, len(r.Hits))
	for _, hit := range r.Hits {
		out = append(out, knowledgetool.RawHit{
			LibraryID: hit.LibraryID, DocumentID: hit.DocumentID, ChunkID: hit.ChunkID,
			Title: hit.Title, Text: hit.Text, SourceURL: hit.SourceURL,
		})
	}
	status := r.Status
	if status == "" {
		if len(out) == 0 {
			status = "empty"
		} else {
			status = "matched"
		}
	}
	h.app.emitKnowledgeToolContext(e, status, out, meta.MessageID)
	return out, status, nil
}

func (a *App) emitKnowledgeSearching(e *sessionEntry, questionID string) {
	if e == nil || e.emit == nil || questionID == "" {
		return
	}
	libs := []KnowledgeBaseSummary{}
	if e.knowledgeTurn != nil {
		for _, lib := range e.knowledgeTurn.snapshot().Libraries {
			libs = append(libs, KnowledgeBaseSummary{ID: lib.ID, Title: lib.Title, Description: lib.Description})
		}
	}
	e.emit(EventKnowledgeContext, KnowledgeContextStatus{
		QuestionID: questionID,
		Knowledge:  QuestionKnowledge{Libraries: libs, Status: "searching", Applied: false},
	})
}

func (a *App) emitKnowledgeToolContext(e *sessionEntry, status string, hits []knowledgetool.RawHit, messageID string) {
	if e == nil || e.emit == nil {
		return
	}
	a.mu.Lock()
	state := e.knowledgeTurn
	questionID := messageID
	libs := []KnowledgeBaseSummary{}
	if state != nil {
		if questionID == "" {
			questionID = state.snap.QuestionID
		}
		for _, lib := range state.snap.Libraries {
			libs = append(libs, KnowledgeBaseSummary{ID: lib.ID, Title: lib.Title, Description: lib.Description})
		}
	}
	a.mu.Unlock()
	sources := make([]KnowledgeSource, 0, len(hits))
	for _, hit := range hits {
		sources = append(sources, KnowledgeSource{
			ID: knowledgetool.SourceID(hit.LibraryID, hit.DocumentID, hit.ChunkID),
			LibraryID: hit.LibraryID, DocumentID: hit.DocumentID, ChunkID: hit.ChunkID,
			Title: hit.Title, SourceURL: hit.SourceURL,
		})
	}
	applied := status == "matched" && len(sources) > 0
	uiStatus := status
	switch status {
	case "matched":
		uiStatus = "matched"
	case "empty":
		uiStatus = "empty"
	default:
		uiStatus = "failed"
	}
	if questionID == "" {
		return
	}
	e.emit(EventKnowledgeContext, KnowledgeContextStatus{
		QuestionID: questionID,
		Knowledge: QuestionKnowledge{
			Libraries: libs,
			Status:    uiStatus,
			Applied:   applied,
			Sources:   sources,
		},
	})
	if state != nil && applied {
		a.mu.Lock()
		if e.knowledgeTurn != nil {
			e.knowledgeTurn.recordSources(questionID, sources)
		}
		a.mu.Unlock()
	}
}

// knowledgeTools returns ExtraTools for knowledge_search when the session can use Bridge knowledge.
func (a *App) knowledgeTools(cfg *engine.Config, sessionID string, emit func(string, any)) []tool.Tool {
	if !usingPassport(cfg) {
		return nil
	}
	if knowledgeDemoEnabled() {
		return nil
	}
	state := newKnowledgeTurnState(sessionID)
	tl := &knowledgetool.Tool{
		Host: knowledgeToolHost{app: a, sessionID: sessionID},
		SnapshotFunc: func() knowledgetool.Snapshot {
			return state.snapshot()
		},
	}
	state.tool = tl
	a.mu.Lock()
	a.pendingKnowledge = state
	a.mu.Unlock()
	_ = emit
	return []tool.Tool{tl}
}
