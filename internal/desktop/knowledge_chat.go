package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	kc "github.com/wt68/runcode/internal/knowledgechat"
	"github.com/wt68/runcode/internal/protocol"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

type knowledgeRun struct {
	id, question string
	cancel       context.CancelFunc
	done         chan struct{}
}

// knowledgeChatSession 与引擎会话互斥。scope 发布后不可变，run/seq 由 App.mu
// 保护；磁盘内容由 Store 自身串行化，网络等待期间不持有 App.mu。
type knowledgeChatSession struct {
	store *kc.Store
	scope kc.Scope
	run   *knowledgeRun
	seq   uint64
	// stopEpoch 记录停止动作，覆盖 receipt 尚未落盘、run 还未登记的准入窗口。
	stopEpoch uint64
}

var errKnowledgeMode = errors.New("知识库问答不支持工具、附件、插入消息或切换连接；请新建普通会话")

func (a *App) checkKnowledgeChat(ctx context.Context, e *sessionEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.knowledgeAccountForRequest(ctx, e); err != nil {
		return err
	}
	pc := passportConfig()
	scope := e.chat.scope
	if scope.Account != e.knowledge.Account || scope.Tenant != e.tenantID || scope.Workspace != e.workspace || scope.KnowledgeBridge != strings.TrimRight(pc.KnowledgeBaseURL, "/") || scope.ModelBridge != strings.TrimRight(pc.BridgeBaseURL, "/") {
		return kc.ErrScope
	}
	return nil
}

// CreateKnowledgeChat 从已验证的平台连接创建独立宿主会话，不复制源聊天历史。
// 请求只能携带源会话 ID 和库 ID，不能注入账号、租户或任意模型服务地址。
func (a *App) CreateKnowledgeChat(req CreateKnowledgeChatRequest) (SessionInfo, error) {
	if strings.TrimSpace(req.SourceSessionID) == "" {
		return SessionInfo{}, wireError(errNoSession)
	}
	source, err := a.entryOf(req.SourceSessionID)
	if err != nil {
		return SessionInfo{}, wireError(err)
	}
	a.mu.Lock()
	cfg, passport, tenant, locked := source.connectionConfig, source.passport, source.tenantID, source.oaLocalModel
	a.mu.Unlock()
	pc := passportConfig()
	if !passport || locked != "" || source.chat != nil || cfg.Model == "" || tenant == "" || strings.TrimRight(cfg.BaseURL, "/") != strings.TrimRight(pc.BridgeBaseURL, "/")+tenantPathPrefix(tenant)+"/v1" {
		return SessionInfo{}, wireError(errKnowledgeMode)
	}
	if cfg.MaxContextTokens <= 0 {
		return SessionInfo{}, wireError(kc.ErrBudget)
	}
	if len(req.LibraryIDs) > 5 {
		return SessionInfo{}, wireError(kc.ErrInput)
	}
	seen := map[string]bool{}
	for _, id := range req.LibraryIDs {
		if !kc.ValidLibrary(id) || seen[id] {
			return SessionInfo{}, wireError(kc.ErrInput)
		}
		seen[id] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.checkKnowledgeAccount(source); err != nil {
		return SessionInfo{}, wireError(err)
	}
	if mode, _ := knowledgeMode(source); mode != "real" {
		return SessionInfo{}, wireError(errKnowledgeMode)
	}
	for _, id := range req.LibraryIDs {
		if _, err := a.knowledgeDetail(ctx, source, id); err != nil {
			return SessionInfo{}, wireError(err)
		}
	}
	scope := kc.Scope{Workspace: source.workspace, Account: source.knowledge.Account, Tenant: tenant, KnowledgeBridge: source.knowledge.Bridge, ModelBridge: strings.TrimRight(pc.BridgeBaseURL, "/")}
	if err := a.authorizeKnowledgeModel(ctx, source, scope, cfg.Model); err != nil {
		return SessionInfo{}, wireError(err)
	}
	d := kc.Document{Version: 1, Kind: kc.Kind, ID: "kc-" + uuid.NewString(), Scope: scope, Model: cfg.Model, Window: cfg.MaxContextTokens, Output: cfg.MaxTokens, Selection: kc.Selection{Revision: 1, LibraryIDs: append([]string{}, req.LibraryIDs...)}, Questions: []kc.Question{}}
	a.startMu.Lock()
	defer a.startMu.Unlock()
	if err := a.checkKnowledgeAccount(source); err != nil {
		return SessionInfo{}, wireError(err)
	}
	store := kc.NewStore(scope.Workspace)
	if err := store.Create(ctx, d); err != nil {
		return SessionInfo{}, wireError(err)
	}
	return a.registerKnowledgeChat(d, cfg, store)
}

func (a *App) registerKnowledgeChat(d kc.Document, cfg engine.Config, store *kc.Store) (SessionInfo, error) {
	a.mu.Lock()
	a.registerSessionModeLocked(d.ID, d.Scope.Workspace, nil, nil, nil, false)
	e := a.entryLocked(d.ID)
	e.chat = &knowledgeChatSession{store: store, scope: d.Scope}
	e.passport = true
	e.tenantID = d.Scope.Tenant
	e.connectionConfig = cfg
	a.bindKnowledgeLocked(e)
	a.focusLocked(e)
	a.mu.Unlock()
	return a.knowledgeChatInfo(e)
}

func (a *App) knowledgeChatInfo(e *sessionEntry) (SessionInfo, error) {
	if err := a.checkKnowledgeHistoryAccess(e); err != nil {
		return SessionInfo{}, wireError(err)
	}
	info := SessionInfo{Kind: kc.Kind, SessionID: e.id, CWD: e.workspace, Model: e.connectionConfig.Model, MaxContextTokens: e.connectionConfig.MaxContextTokens}
	// 相同账号可离线查看已保存回答；服务边界变化时只读，不能转发旧资料。
	pc := passportConfig()
	if e.chat.scope.KnowledgeBridge != strings.TrimRight(pc.KnowledgeBaseURL, "/") || e.chat.scope.ModelBridge != strings.TrimRight(pc.BridgeBaseURL, "/") {
		info.ReadOnlyReason = "服务地址已变化；此会话仅可查看，请新建知识库问答"
	}
	return info, nil
}

func (a *App) submitKnowledgeChat(req SubmitQuestionRequest) (QuestionReceipt, error) {
	e, err := a.entryOf(req.SessionID)
	if err != nil {
		return QuestionReceipt{}, wireError(err)
	}
	if e.chat == nil {
		return QuestionReceipt{}, wireError(kc.ErrFormat)
	}
	if req.AllowSteering || len(req.ImagePaths) > 0 || len(req.OriginalImages) > 0 {
		return QuestionReceipt{}, wireError(errKnowledgeMode)
	}
	if req.KnowledgeRevision == nil {
		return QuestionReceipt{}, wireError(kc.ErrRevision)
	}
	if os.Getenv("RUNCODE_SSE_DUMP_DIR") != "" {
		return QuestionReceipt{}, wireError(errors.New("请清除原始模型流抓包配置后使用知识库问答"))
	}
	a.mu.Lock()
	stopEpoch := e.chat.stopEpoch
	a.mu.Unlock()
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	a.mu.Lock()
	closed := e.closed || a.entryLocked(e.id) != e
	a.mu.Unlock()
	if closed {
		return QuestionReceipt{}, wireError(errNoSession)
	}
	if err := a.checkKnowledgeChat(context.Background(), e); err != nil {
		return QuestionReceipt{}, wireError(err)
	}
	// 重试只能引用经验证的分支父题；nil 与显式空选库分别表示继承和清空。
	input, err := a.knowledgeChatSubmitInput(context.Background(), e, req)
	if err != nil {
		return QuestionReceipt{}, wireError(err)
	}
	accepted, repeated, err := e.chat.store.Accept(context.Background(), e.id, e.chat.scope, input)
	if err != nil {
		return QuestionReceipt{}, wireError(err)
	}
	receipt := QuestionReceipt{QuestionID: accepted.Question.ID, StartedTurn: true}
	if repeated {
		return receipt, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &knowledgeRun{id: uuid.NewString(), question: accepted.Question.ID, cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	if e.chat.stopEpoch != stopEpoch {
		// 准入写盘期间发生停止：保留已接受记录，但后台立即写 cancelled，零网络。
		cancel()
	}
	e.chat.run = run
	e.turnActive = true
	a.mu.Unlock()
	// 返回持久 receipt 前先登记可取消句柄，关闭/退出可立即终止工作。
	// 后台任务依附 App 生命周期，事件允许先于 IPC 返回，前端按 questionId 对齐。
	go a.runKnowledgeAnswer(ctx, e, run, accepted)
	return receipt, nil
}

func (a *App) emitKnowledgeChat(e *sessionEntry, run *knowledgeRun, phase string) {
	a.emitKnowledgeChatEvent(e, run, protocol.KnowledgeChatEvent{Phase: phase})
}

// 所有宿主事件共享会话序列号。关闭、换账号、旧运行迟到时直接丢弃，
// 前端还会独立核对 sessionId/runId/seq，防止切换标签后串入旧答案。
func (a *App) emitKnowledgeChatEvent(e *sessionEntry, run *knowledgeRun, payload protocol.KnowledgeChatEvent) {
	if !a.knowledgeEventIdentity(e, run, kc.Terminal(payload.Phase)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.closed || a.entryLocked(e.id) != e || e.chat.run != run {
		return
	}
	e.chat.seq++
	payload.SessionID, payload.QuestionID, payload.RunID, payload.Seq = e.id, run.question, run.id, e.chat.seq
	a.out.Emit(protocol.EventKnowledgeChat, hostproto.Envelope{Event: protocol.EventKnowledgeChat, SessionID: e.id, Seq: e.chat.seq, TS: time.Now().Format(time.RFC3339Nano), Payload: payload})
}

// 续期持有 token 锁时不能把一次性的终态误当成账号撤销直接丢弃。
// 终态最多等待一次令牌请求的 30 秒期限加调度余量；关闭/换会话立即停止等待。
// 正文只在再次确认同一身份后投递，绝不以忽略身份检查来消除卡忙。
func (a *App) knowledgeEventIdentity(e *sessionEntry, run *knowledgeRun, terminal bool) bool {
	deadline := time.NewTimer(32 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		a.mu.Lock()
		active := !e.closed && a.entryLocked(e.id) == e && e.chat.run == run
		a.mu.Unlock()
		if !active {
			return false
		}
		err := a.checkKnowledgeAccount(e)
		if err == nil {
			return true
		}
		if !terminal || !errors.Is(err, errKnowledgeUnavailable) {
			return false
		}
		select {
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}

func (a *App) stopKnowledgeChat(ctx context.Context, e *sessionEntry) error {
	e.questionMu.Lock()
	a.mu.Lock()
	run := e.chat.run
	if run != nil {
		run.cancel()
	}
	a.mu.Unlock()
	e.questionMu.Unlock()
	if run != nil {
		select {
		case <-run.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (a *App) knowledgeChatDependencies(e *sessionEntry) kc.Dependencies {
	return kc.Dependencies{
		Check: func(ctx context.Context, scope kc.Scope) error {
			if scope != e.chat.scope {
				return kc.ErrScope
			}
			return a.checkKnowledgeChat(ctx, e)
		},
		Retrieve: func(ctx context.Context, _ kc.Scope, ids []string, text string) (kc.Retrieved, error) {
			payload, _ := json.Marshal(struct {
				LibraryIDs []string `json:"libraryIds"`
				Question   string   `json:"question"`
			}{ids, text})
			var r kc.Retrieved
			err := a.knowledgeRequest(ctx, e, http.MethodPost, "/retrieve", payload, &r)
			return r, err
		},
		AuthorizeLibrary: func(ctx context.Context, _ kc.Scope, id string) error {
			_, err := a.knowledgeDetail(ctx, e, id)
			return err
		},
		AuthorizeModel: func(ctx context.Context, scope kc.Scope, model string) error {
			return a.authorizeKnowledgeModel(ctx, e, scope, model)
		},
	}
}

func (a *App) authorizeKnowledgeModel(ctx context.Context, e *sessionEntry, scope kc.Scope, model string) error {
	if scope.ModelBridge != strings.TrimRight(passportConfig().BridgeBaseURL, "/") || scope.Tenant == "" || scope.Tenant != e.tenantID {
		return kc.ErrScope
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	endpoint := scope.ModelBridge + tenantPathPrefix(scope.Tenant) + "/v1/models"
	if err := a.knowledgeJSON(ctx, e, http.MethodGet, endpoint, nil, &payload); err != nil {
		return err
	}
	for _, item := range payload.Data {
		if item.ID == model {
			return nil
		}
	}
	return errKnowledgeForbidden
}

func (a *App) getKnowledgeChatSelection(e *sessionEntry) (SessionKnowledgeView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	if err := a.checkKnowledgeChat(ctx, e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	d, err := e.chat.store.Load(ctx, e.id)
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	view := a.knowledgeView(ctx, e, knowledgeSelection{Revision: d.Selection.Revision, LibraryIDs: d.Selection.LibraryIDs}, true)
	if err := a.checkKnowledgeChat(ctx, e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	return view, nil
}

func (a *App) setKnowledgeChatSelection(e *sessionEntry, req SetSessionKnowledgeRequest) (SessionKnowledgeView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	if err := a.checkKnowledgeChat(ctx, e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	if len(req.LibraryIDs) > 5 {
		return SessionKnowledgeView{}, wireError(kc.ErrInput)
	}
	ids := append([]string{}, req.LibraryIDs...)
	sort.Strings(ids)
	libraries := make([]KnowledgeBaseSummary, 0, len(ids))
	for i, id := range ids {
		if !kc.ValidLibrary(id) || (i > 0 && ids[i-1] == id) {
			return SessionKnowledgeView{}, wireError(kc.ErrInput)
		}
		library, err := a.knowledgeDetail(ctx, e, id)
		if err != nil {
			return SessionKnowledgeView{}, wireError(err)
		}
		libraries = append(libraries, library)
	}
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	if err := a.checkKnowledgeChat(ctx, e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	err := e.chat.store.Update(ctx, e.id, e.chat.scope, func(d *kc.Document) error {
		if d.Selection.Revision != req.ExpectedRevision {
			return kc.ErrRevision
		}
		if err := kc.CheckLibraryLimit(*d, ids); err != nil {
			return err
		}
		d.Selection = kc.Selection{Revision: d.Selection.Revision + 1, LibraryIDs: ids}
		return nil
	})
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	// 复用本次已授权详情，不再请求一遍，也不能把已验证标签标为不可用。
	if err := a.checkKnowledgeChat(ctx, e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	return SessionKnowledgeView{SessionID: e.id, Revision: req.ExpectedRevision + 1, Libraries: libraries, Mode: "real"}, nil
}
