package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const knowledgeRequestTimeout = 10 * time.Second

var errKnowledgeUnavailable = errors.New("知识库目录暂不可用，请稍后重试")
var errKnowledgeAuthentication = errors.New("通行证登录已失效，请重新登录后重试")
var errKnowledgeForbidden = errors.New("当前账号无权访问此知识库")

type knowledgeRefreshKey struct{}

// 一道知识库问题的检索、重新授权和模型请求共享一次强制刷新额度。
// 普通目录请求沿用已有逻辑；不改动普通聊天的 tokenManager 行为。
func withKnowledgeRefreshBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, knowledgeRefreshKey{}, new(atomic.Bool))
}

// knowledgeAccountForRequest waits for the token mutex under ctx.
// Lifecycle paths keep TryLock via checkKnowledgeAccount so UI catalog work
// fails closed quickly; Bridge request I/O must wait out an in-flight refresh
// instead of spinning for 100ms and failing while the refresh still holds the lock.
func (a *App) knowledgeAccountForRequest(ctx context.Context, e *sessionEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.tokens == nil {
		return a.checkKnowledgeEntry(e)
	}
	done := make(chan error, 1)
	go func() {
		a.tokens.mu.Lock()
		account := "anonymous"
		if a.tokens.ts.AccessToken != "" || a.tokens.ts.RefreshToken != "" {
			account = tokenIdentity(a.tokens.ts.AccessToken)
		}
		current := e.knowledge.Epoch == a.tokens.identity && e.knowledge.Account == account
		a.tokens.mu.Unlock()
		if !current {
			done <- errors.New("知识库会话账号已变化，请重新打开会话")
			return
		}
		done <- a.checkKnowledgeEntry(e)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func validRealKnowledgeID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func validStoredKnowledgeID(id string) bool {
	if validRealKnowledgeID(id) {
		return true
	}
	if !strings.HasPrefix(id, "demo:") || len(id) <= 5 || len(id) > 100 {
		return false
	}
	for _, ch := range id[5:] {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-':
		default:
			return false
		}
	}
	return true
}

// Token and ForceRefresh serialize rotating grants internally. Waiting here is
// bounded by the whole operation deadline even if a refresh outlives that wait.
func (a *App) knowledgeToken(ctx context.Context, e *sessionEntry, refresh bool) (string, error) {
	if budget, ok := ctx.Value(knowledgeRefreshKey{}).(*atomic.Bool); refresh && ok && !budget.CompareAndSwap(false, true) {
		return "", errKnowledgeAuthentication
	}
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		if err := a.knowledgeAccountForRequest(ctx, e); err != nil {
			done <- result{err: err}
			return
		}
		if refresh {
			a.tokens.ForceRefresh()
		}
		token, err := a.tokens.Token()
		done <- result{token: token, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", errKnowledgeUnavailable
	case result := <-done:
		if result.err != nil {
			if errors.Is(result.err, errNotLoggedIn) {
				return "", errKnowledgeAuthentication
			}
			return "", errKnowledgeUnavailable
		}
		if err := a.knowledgeAccountForRequest(ctx, e); err != nil {
			return "", err
		}
		return result.token, nil
	}
}

func (a *App) knowledgeGET(ctx context.Context, e *sessionEntry, suffix string, target any) error {
	return a.knowledgeRequest(ctx, e, http.MethodGet, suffix, nil, target)
}
func (a *App) knowledgeRequest(ctx context.Context, e *sessionEntry, method, suffix string, payload []byte, target any) error {
	if e.knowledge.Tenant == "" || e.knowledge.Bridge == "" || a.tokens == nil {
		return errKnowledgeUnavailable
	}
	base, err := url.Parse(e.knowledge.Bridge)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return errKnowledgeUnavailable
	}
	endpoint := e.knowledge.Bridge + "/t/" + url.PathEscape(e.knowledge.Tenant) + "/v1/knowledge-bases" + suffix
	return a.knowledgeJSON(ctx, e, method, endpoint, payload, target)
}

// 只允许后台按已绑定服务地址构造的知识库/模型端点调用，不接受前端任意 URL。
func (a *App) knowledgeJSON(ctx context.Context, e *sessionEntry, method, endpoint string, payload []byte, target any) error {
	if len(payload) > 1<<20 {
		return errKnowledgeUnavailable
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := a.knowledgeToken(ctx, e, attempt == 1)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return errKnowledgeUnavailable
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return errKnowledgeUnavailable
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		_ = resp.Body.Close()
		if readErr != nil || len(body) > 1<<20 {
			return errKnowledgeUnavailable
		}
		if err := a.knowledgeAccountForRequest(ctx, e); err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return errKnowledgeAuthentication
		}
		if resp.StatusCode == http.StatusForbidden {
			return errKnowledgeForbidden
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, target) != nil {
			return errKnowledgeUnavailable
		}
		return nil
	}
	return errKnowledgeUnavailable
}

func sanitizeKnowledgeSummary(library *KnowledgeBaseSummary, expectedID string) error {
	if !validRealKnowledgeID(library.ID) || (expectedID != "" && library.ID != expectedID) || strings.TrimSpace(library.Title) == "" || library.DocumentCount < 0 || library.UnavailableReason != "" {
		return errKnowledgeUnavailable
	}
	if !validKnowledgeDirectoryURL(library.DirectoryURL, library.ID) {
		library.DirectoryURL = ""
	}
	return nil
}

func (a *App) knowledgeDetail(ctx context.Context, e *sessionEntry, id string) (KnowledgeBaseSummary, error) {
	if !validRealKnowledgeID(id) {
		return KnowledgeBaseSummary{}, errKnowledgeUnavailable
	}
	var summary KnowledgeBaseSummary
	if err := a.knowledgeGET(ctx, e, "/"+id, &summary); err != nil {
		return KnowledgeBaseSummary{}, err
	}
	if err := sanitizeKnowledgeSummary(&summary, id); err != nil {
		return KnowledgeBaseSummary{}, err
	}
	return summary, nil
}

func validKnowledgeDirectoryURL(raw, id string) bool {
	if !validRealKnowledgeID(id) {
		return false
	}
	// The portal is deployment configuration, never an origin supplied by a
	// library response. Test services must link to their own test directory.
	portal := strings.TrimRight(envOr("RUNCODE_KNOWLEDGE_PORTAL_BASE_URL", "https://ai.ouchn.edu.cn"), "/")
	origin, err := url.Parse(portal)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Host != origin.Hostname() ||
		origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	expected := portal + "/knowledge-base/" + id + "/data-collection"
	// Exact canonical spelling also rejects userinfo, ports, encoded paths,
	// queries, fragments and backslash normalization at the OS boundary.
	if raw != expected {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == origin.Host && parsed.User == nil && parsed.RawPath == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
