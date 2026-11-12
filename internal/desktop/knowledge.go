package desktop

import (
	"context"
	"errors"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const knowledgeMaxSelection = 5

const knowledgeDemoNotice = "合成测试目录，仅用于验证选择与保存；检索尚未接通，选库后暂不能发送"

func demoKnowledgeCatalog() []KnowledgeBaseSummary {
	return []KnowledgeBaseSummary{
		{ID: "demo:employee-01", Title: "数字员工01（测试示例）", Description: "合成测试资料：员工服务与工单问答示例，不含真实业务数据", Path: "测试目录 / 数字员工01", Source: "demo", DocumentCount: 3, DocumentCountKnown: true},
		{ID: "demo:policies", Title: "制度指南（测试示例）", Description: "合成测试资料：制度流程与常见问题", Path: "测试目录 / 制度指南", Source: "demo", DocumentCount: 5, DocumentCountKnown: true},
		{ID: "demo:products", Title: "产品手册（测试示例）", Description: "合成测试资料：产品说明与使用方法", Path: "测试目录 / 产品手册", Source: "demo", DocumentCount: 2, DocumentCountKnown: true},
		{ID: "demo:onboarding", Title: "入职培训（测试示例）", Description: "合成测试资料：培训指引与学习清单", Path: "测试目录 / 入职培训", Source: "demo", DocumentCount: 4, DocumentCountKnown: true},
		{ID: "demo:it-support", Title: "IT服务（测试示例）", Description: "合成测试资料：设备支持与服务申请指引", Path: "测试目录 / IT服务", Source: "demo", DocumentCount: 6, DocumentCountKnown: true},
		{ID: "demo:finance", Title: "财务报销（测试示例）", Description: "合成测试资料：报销步骤与票据检查清单", Path: "测试目录 / 财务报销", Source: "demo", DocumentCount: 3, DocumentCountKnown: true},
		{ID: "demo:hr-services", Title: "人事服务（测试示例）", Description: "合成测试资料：考勤说明与员工服务流程", Path: "测试目录 / 人事服务", Source: "demo", DocumentCount: 4, DocumentCountKnown: true},
		{ID: "demo:customer-service", Title: "客户服务（测试示例）", Description: "合成测试资料：咨询处理与服务沟通示例", Path: "测试目录 / 客户服务", Source: "demo", DocumentCount: 5, DocumentCountKnown: true},
		{ID: "demo:project-process", Title: "项目流程（测试示例）", Description: "合成测试资料：项目立项、交付与验收清单", Path: "测试目录 / 项目流程", Source: "demo", DocumentCount: 7, DocumentCountKnown: true},
		{ID: "demo:security", Title: "信息安全（测试示例）", Description: "合成测试资料：安全常识与数据保护指引", Path: "测试目录 / 信息安全", Source: "demo", DocumentCount: 3, DocumentCountKnown: true},
		{ID: "demo:procurement", Title: "采购指南（测试示例）", Description: "合成测试资料：采购申请与材料准备说明", Path: "测试目录 / 采购指南", Source: "demo", DocumentCount: 4, DocumentCountKnown: true},
		{ID: "demo:learning-resources", Title: "学习资源（测试示例）", Description: "合成测试资料：课程介绍与学习资料索引", Path: "测试目录 / 学习资源", Source: "demo", DocumentCount: 8, DocumentCountKnown: true},
	}
}

func demoKnowledgeByID(id string) (KnowledgeBaseSummary, bool) {
	for _, library := range demoKnowledgeCatalog() {
		if library.ID == id {
			return library, true
		}
	}
	return KnowledgeBaseSummary{}, false
}

// knowledgeBinding is immutable after publication. Epoch is in memory only;
// the stable account fingerprint partitions disk records across app restarts.
type knowledgeBinding struct{ Account, Epoch, Bridge, Tenant string }

func (a *App) knowledgeIdentity() (account, epoch string) {
	if a.tokens == nil {
		return "anonymous", ""
	}
	a.tokens.mu.Lock()
	defer a.tokens.mu.Unlock()
	epoch = a.tokens.identity
	if a.tokens.ts.AccessToken == "" && a.tokens.ts.RefreshToken == "" {
		return "anonymous", epoch
	}
	return tokenIdentity(a.tokens.ts.AccessToken), epoch
}

// Caller holds App.mu while publishing a newly constructed entry.
func (a *App) bindKnowledgeLocked(e *sessionEntry) {
	account, epoch := a.knowledgeIdentity()
	// The knowledge service is the Passport Bridge, independent of whichever
	// provider receives generated text. Model switches must retain selection.
	e.knowledge = knowledgeBinding{Account: account, Epoch: epoch, Bridge: strings.TrimRight(passportConfig().KnowledgeBaseURL, "/"), Tenant: e.tenantID}
}

func (a *App) checkKnowledgeEntry(e *sessionEntry) error {
	a.mu.Lock()
	current := a.entryLocked(e.id) == e && !e.closed
	a.mu.Unlock()
	if !current {
		return errors.New("知识库会话或账号已变化，请重新打开会话")
	}
	return nil
}

func (a *App) checkKnowledgeAccount(e *sessionEntry) error {
	if a.tokens != nil {
		// A refresh may hold this mutex over network IO. Fail closed rather than
		// making lifecycle checks wait past the catalog deadline.
		if !a.tokens.mu.TryLock() {
			return errKnowledgeUnavailable
		}
		account := "anonymous"
		if a.tokens.ts.AccessToken != "" || a.tokens.ts.RefreshToken != "" {
			account = tokenIdentity(a.tokens.ts.AccessToken)
		}
		current := e.knowledge.Epoch == a.tokens.identity && e.knowledge.Account == account
		a.tokens.mu.Unlock()
		if !current {
			return errors.New("知识库会话账号已变化，请重新打开会话")
		}
	}
	return a.checkKnowledgeEntry(e)
}

func knowledgeDemoEnabled() bool {
	return IsTestBuild() && os.Getenv("RUNCODE_KNOWLEDGE_MODE") == "demo"
}

func knowledgeScope(e *sessionEntry) string {
	version := "knowledge-real-v1"
	if knowledgeDemoEnabled() {
		version = "knowledge-demo-v1"
	}
	return digestVision([]string{version, e.knowledge.Account, e.knowledge.Bridge, e.knowledge.Tenant})
}

func knowledgeMode(e *sessionEntry) (string, string) {
	if e.knowledge.Account == "" {
		return "unavailable", "无法确认当前账号的稳定身份，知识库选择暂不可用；请重新登录"
	}
	if knowledgeDemoEnabled() {
		return "demo", knowledgeDemoNotice
	}
	if e.knowledge.Account == "anonymous" {
		return "unavailable", "请先登录通行证后使用知识库"
	}
	if e.knowledge.Tenant == "" || e.knowledge.Bridge == "" {
		return "unavailable", "请选择知识库所属租户后重新打开会话"
	}
	return "real", ""
}

func (a *App) knowledgeView(ctx context.Context, e *sessionEntry, selection knowledgeSelection, revalidate bool) SessionKnowledgeView {
	return a.knowledgeViewWithDetails(ctx, e, selection, revalidate, nil)
}

// knowledgeViewWithDetails 可复用同一次保存里已授权的详情，避免新增库被读两次。
// details 只活在本请求；跨请求仍走实时 knowledgeDetail。
func (a *App) knowledgeViewWithDetails(ctx context.Context, e *sessionEntry, selection knowledgeSelection, revalidate bool, details map[string]KnowledgeBaseSummary) SessionKnowledgeView {
	mode, reason := knowledgeMode(e)
	view := SessionKnowledgeView{SessionID: e.id, Revision: selection.Revision, Libraries: []KnowledgeBaseSummary{}, Mode: mode, UnavailableReason: reason}
	for _, id := range selection.LibraryIDs {
		var library KnowledgeBaseSummary
		var err error
		switch {
		case mode == "demo":
			var ok bool
			library, ok = demoKnowledgeByID(id)
			if !ok {
				err = errKnowledgeUnavailable
			}
		case mode == "real" && revalidate:
			if cached, ok := details[id]; ok {
				library = cached
			} else {
				library, err = a.knowledgeDetail(ctx, e, id)
			}
		default:
			err = errKnowledgeUnavailable
		}
		if err != nil {
			library = KnowledgeBaseSummary{ID: id, Title: "不可用的知识库", UnavailableReason: "无法确认访问权限，请重试或移除此知识库"}
		}
		view.Libraries = append(view.Libraries, library)
	}
	return view
}

// ListKnowledgeBases lists the explicit session's authenticated catalog.
func (a *App) ListKnowledgeBases(req KnowledgeListRequest) (KnowledgePage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	e, err := a.explicitKnowledgeEntry(req.SessionID)
	if err != nil {
		return KnowledgePage{}, wireError(err)
	}
	if err := a.checkKnowledgeAccount(e); err != nil {
		return KnowledgePage{}, wireError(err)
	}
	mode, reason := knowledgeMode(e)
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 50 {
		size = 50
	}
	result := KnowledgePage{Items: []KnowledgeBaseSummary{}, Page: page, PageSize: size, Mode: mode, UnavailableReason: reason, MaxSelection: knowledgeMaxSelection}
	if mode == "demo" {
		query := strings.ToLower(strings.TrimSpace(req.Query))
		items := []KnowledgeBaseSummary{}
		for _, library := range demoKnowledgeCatalog() {
			if query == "" || strings.Contains(strings.ToLower(library.Title+" "+library.Description+" "+library.Path), query) {
				items = append(items, library)
			}
		}
		result.Total = len(items)
		if page <= (len(items)+size-1)/size {
			start := (page - 1) * size
			result.Items = items[start:min(start+size, len(items))]
			result.HasMore = start+size < len(items)
		}
	} else if mode == "real" {
		var catalog struct {
			Items    []KnowledgeBaseSummary `json:"items"`
			Page     int                    `json:"page"`
			PageSize int                    `json:"pageSize"`
			Total    *int                   `json:"total"`
			HasMore  *bool                  `json:"hasMore"`
		}
		values := url.Values{"query": {strings.TrimSpace(req.Query)}, "page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(size)}}
		err := a.knowledgeGET(ctx, e, "?"+values.Encode(), &catalog)
		if err == nil && (catalog.Items == nil || catalog.Page != page || catalog.PageSize != size || catalog.Total == nil || *catalog.Total < -1 || catalog.HasMore == nil || len(catalog.Items) > size) {
			err = errKnowledgeUnavailable
		}
		seen := map[string]bool{}
		if err == nil {
			for i := range catalog.Items {
				if sanitizeKnowledgeSummary(&catalog.Items[i], "") != nil || seen[catalog.Items[i].ID] {
					err = errKnowledgeUnavailable
					break
				}
				seen[catalog.Items[i].ID] = true
			}
		}
		if err != nil {
			result.Mode, result.UnavailableReason, result.FailureKind = "unavailable", err.Error(), knowledgeFailureKind(err)
		} else {
			result.Items, result.Total, result.HasMore = catalog.Items, *catalog.Total, *catalog.HasMore
		}
	}
	if err := a.checkKnowledgeAccount(e); err != nil {
		return KnowledgePage{}, knowledgeWireError(err)
	}
	return result, nil
}

func (a *App) explicitKnowledgeEntry(sessionID string) (*sessionEntry, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errNoSession
	}
	e, err := a.entryOf(sessionID)
	if err != nil {
		return nil, err
	}
	if err := a.checkKnowledgeEntry(e); err != nil {
		return nil, err
	}
	return e, nil
}

// GetSessionKnowledge reads and revalidates the session's durable selection.
func (a *App) GetSessionKnowledge(sessionID string) (SessionKnowledgeView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	e, err := a.explicitKnowledgeEntry(sessionID)
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	e.questionMu.Lock()
	disk, err := readKnowledge(e.workspace, e.id)
	e.questionMu.Unlock()
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	selection := disk.Selections[knowledgeScope(e)]
	if err := a.checkKnowledgeAccount(e); err != nil {
		if len(selection.LibraryIDs) > 0 {
			return SessionKnowledgeView{}, wireError(err)
		}
		// Ordinary custom-provider chat does not depend on Passport login.
		if entryErr := a.checkKnowledgeEntry(e); entryErr != nil {
			return SessionKnowledgeView{}, wireError(entryErr)
		}
		return SessionKnowledgeView{SessionID: e.id, Revision: selection.Revision, Libraries: []KnowledgeBaseSummary{}, Mode: "unavailable", UnavailableReason: err.Error()}, nil
	}
	view := a.knowledgeView(ctx, e, selection, true)
	if err := a.checkKnowledgeAccount(e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	return view, nil
}

// SetSessionKnowledge performs authorization without lifecycle/storage locks,
// then reacquires the existing lock order and CAS before durable publication.
func (a *App) SetSessionKnowledge(req SetSessionKnowledgeRequest) (SessionKnowledgeView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	e, err := a.explicitKnowledgeEntry(req.SessionID)
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	if err := a.checkKnowledgeAccount(e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	e.questionMu.Lock()
	disk, err := readKnowledge(e.workspace, e.id)
	e.questionMu.Unlock()
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	scope := knowledgeScope(e)
	previous := disk.Selections[scope]
	if previous.Revision != req.ExpectedRevision {
		return SessionKnowledgeView{}, wireError(errors.New("知识库选择已变化，请刷新后重试"))
	}
	ids := []string{}
	seen, old := map[string]bool{}, map[string]bool{}
	for _, id := range previous.LibraryIDs {
		old[id] = true
	}
	for _, id := range req.LibraryIDs {
		if !validStoredKnowledgeID(id) {
			return SessionKnowledgeView{}, wireError(errors.New("包含未知的知识库，请重新选择"))
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > knowledgeMaxSelection {
		return SessionKnowledgeView{}, wireError(errors.New("每个会话最多选择 5 个知识库"))
	}
	mode, reason := knowledgeMode(e)
	hasAdditions := false
	// 本次新增库的授权详情只在本请求内复用，不进目录缓存或会话存储。
	addedDetails := map[string]KnowledgeBaseSummary{}
	for _, id := range ids {
		if old[id] {
			continue
		} // Clear and subset removal never need the gateway.
		hasAdditions = true
		switch mode {
		case "demo":
			library, ok := demoKnowledgeByID(id)
			if !ok {
				return SessionKnowledgeView{}, wireError(errors.New("包含未知的知识库，请重新选择"))
			}
			addedDetails[id] = library
		case "real":
			library, err := a.knowledgeDetail(ctx, e, id)
			if err != nil {
				return SessionKnowledgeView{}, wireError(err)
			}
			addedDetails[id] = library
		default:
			return SessionKnowledgeView{}, wireError(errors.New(reason))
		}
	}
	sort.Strings(ids)
	oldIDs := append([]string{}, previous.LibraryIDs...)
	sort.Strings(oldIDs)
	selection := previous
	if !reflect.DeepEqual(ids, oldIDs) {
		selection = knowledgeSelection{Revision: previous.Revision + 1, LibraryIDs: ids}
	}
	// Removing retained real IDs must not ask Token() to refresh or clear an
	// expired login before the local CAS can persist the removal. Explicit Get
	// revalidates their metadata; until then keep conservative placeholders.
	// 新增库复用上面刚授权的详情，避免同一次保存再读一遍。
	view := a.knowledgeViewWithDetails(ctx, e, selection, hasAdditions, addedDetails)
	a.startMu.Lock()
	defer a.startMu.Unlock()
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	disk, err = readKnowledge(e.workspace, e.id)
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	if disk.Selections[scope].Revision != req.ExpectedRevision {
		return SessionKnowledgeView{}, wireError(errors.New("知识库选择已变化，请刷新后重试"))
	}
	// Account changes and entry replacement cannot race the final disk rename.
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.entryLocked(e.id) != e || e.closed {
		return SessionKnowledgeView{}, wireError(errors.New("知识库会话已变化，请重新打开会话"))
	}
	if a.tokens != nil {
		if !a.tokens.mu.TryLock() {
			return SessionKnowledgeView{}, wireError(errKnowledgeUnavailable)
		}
		defer a.tokens.mu.Unlock()
		account := "anonymous"
		if a.tokens.ts.AccessToken != "" || a.tokens.ts.RefreshToken != "" {
			account = tokenIdentity(a.tokens.ts.AccessToken)
		}
		if e.knowledge.Epoch != a.tokens.identity || e.knowledge.Account != account {
			return SessionKnowledgeView{}, wireError(errors.New("知识库会话账号已变化，请重新打开会话"))
		}
	}
	if selection.Revision != previous.Revision {
		selection.Summaries = knowledgeSummariesFromView(view.Libraries)
		disk.Version = knowledgeStoreVersionV2
		disk.Selections[scope] = selection
		if err := writeKnowledge(e.workspace, e.id, disk); err != nil {
			return SessionKnowledgeView{}, wireError(err)
		}
	}
	return view, nil
}

// GetSessionKnowledgeSnapshot 只读本地已确认选择与标题摘要，不发 HTTP。
func (a *App) GetSessionKnowledgeSnapshot(sessionID string) (SessionKnowledgeView, error) {
	e, err := a.explicitKnowledgeEntry(sessionID)
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	if err := a.checkKnowledgeEntry(e); err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	e.questionMu.Lock()
	disk, err := readKnowledge(e.workspace, e.id)
	e.questionMu.Unlock()
	if err != nil {
		return SessionKnowledgeView{}, wireError(err)
	}
	selection := disk.Selections[knowledgeScope(e)]
	return knowledgeViewFromSnapshot(e.id, selection), nil
}

// ensureKnowledgeSendable rejects turns when the selection store is unreadable
// or a non-empty selection's account binding no longer matches. Empty selection
// still allows ordinary chat across Passport churn (same rule as GetSessionKnowledge).
// It does not require a revision CAS and does not take startMu. History-format
// checks stay on Resume/build.
func (a *App) ensureKnowledgeSendable(e *sessionEntry) error {
	disk, err := readKnowledge(e.workspace, e.id)
	if err != nil {
		return err
	}
	selection := disk.Selections[knowledgeScope(e)]
	if err := a.checkKnowledgeAccount(e); err != nil {
		if len(selection.LibraryIDs) > 0 {
			return err
		}
		return a.checkKnowledgeEntry(e)
	}
	return nil
}

// checkKnowledgeRevision is ensureKnowledgeSendable plus optional selection CAS.
func (a *App) checkKnowledgeRevision(e *sessionEntry, expected *int64) error {
	if err := a.ensureKnowledgeSendable(e); err != nil {
		return err
	}
	if expected == nil {
		return nil
	}
	disk, err := readKnowledge(e.workspace, e.id)
	if err != nil {
		return err
	}
	if *expected != disk.Selections[knowledgeScope(e)].Revision {
		return errors.New("知识库选择版本已变化，请确认选择后重新发送")
	}
	return nil
}
