package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const knowledgeTestID = "11111111-2222-4333-8444-555555555555"
const knowledgeTestID2 = "22222222-2222-4333-8444-555555555555"

func knowledgeJWT(sub string) string {
	return "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"knowledge-test","sub":"`+sub+`"}`)) + ".x"
}

func realKnowledgeApp(t *testing.T, handler http.HandlerFunc) (*App, string) {
	t.Helper()
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "")
	isolateConfigDir(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	// 知识库桥可单独覆盖；本机若残留 RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL 会打到真服务拿 401。
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", server.URL)
	t.Setenv("RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL", server.URL)
	app, _, built := newParallelApp(t)
	t.Cleanup(func() { _ = app.mgr.CloseAll(context.Background()) })
	app.tokens.setInMemory(tokenSet{AccessToken: knowledgeJWT("A"), Expiry: time.Now().Add(time.Hour)})
	id, _ := openParallelSession(t, app, built)
	app.mu.Lock()
	e := app.entryLocked(id)
	e.tenantID = "tenant-A"
	app.bindKnowledgeLocked(e)
	app.mu.Unlock()
	return app, id
}

func knowledgeSummaryJSON(id string) string {
	return `{"id":"` + id + `","title":"Authorized library","description":"Description","path":"/knowledge-base/` + id + `/data-collection","source":"基座知识库","documentCount":0,"documentCountKnown":false,"directoryUrl":"https://ai.ouchn.edu.cn/knowledge-base/` + id + `/data-collection"}`
}

func TestKnowledgeRealCatalogDefaultAuthenticatedAndEmptyContinuation(t *testing.T) {
	var calls atomic.Int32
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/t/tenant-A/v1/knowledge-bases" || r.URL.Query().Get("query") != "a & b" || r.URL.Query().Get("page") != "2" || r.Header.Get("Authorization") != "Bearer "+knowledgeJWT("A") {
			t.Errorf("wrong authenticated catalog request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[],"page":2,"pageSize":10,"total":-1,"hasMore":true}`))
	})
	page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id, Query: "a & b", Page: 2, PageSize: 10})
	data, _ := json.Marshal(page)
	if err != nil || page.Mode != "real" || page.Total != -1 || calls.Load() != 1 || !strings.Contains(string(data), `"hasMore":true`) {
		t.Fatalf("real catalog/empty continuation lost: %+v %v", page, err)
	}
}

func TestKnowledgeRealUnavailableNeverFallsBackToDemo(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("secret upstream document and token"))
			})
			page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
			if len(page.Items) != 0 || page.Mode == "demo" || (err == nil && page.Mode != "unavailable") || strings.Contains(page.UnavailableReason, "secret") || (err != nil && strings.Contains(err.Error(), "secret")) {
				t.Fatalf("failure leaked or fabricated catalog: %+v %v", page, err)
			}
			if status == 401 && !strings.Contains(page.UnavailableReason, "重新登录") {
				t.Fatalf("expired login not actionable: %+v", page)
			}
			if status == 403 && !strings.Contains(page.UnavailableReason, "无权") {
				t.Fatalf("forbidden access misreported: %+v", page)
			}
		})
	}
}

func TestKnowledgeRealSelectionRevalidatesAndCanRemoveRevokedOffline(t *testing.T) {
	var unavailable atomic.Bool
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/t/tenant-A/v1/knowledge-bases/"+knowledgeTestID {
			t.Errorf("unexpected detail %s", r.URL)
		}
		_, _ = w.Write([]byte(knowledgeSummaryJSON(knowledgeTestID)))
	})
	view, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}})
	if err != nil || view.Revision != 1 || len(view.Libraries) != 1 || view.Libraries[0].Title != "Authorized library" {
		t.Fatalf("authorized selection: %+v %v", view, err)
	}
	unavailable.Store(true)
	view, err = app.GetSessionKnowledge(id)
	data, _ := json.Marshal(view)
	if err != nil || len(view.Libraries) != 1 || view.Libraries[0].ID != knowledgeTestID || !strings.Contains(string(data), `"unavailableReason"`) || strings.Contains(string(data), "https://") {
		t.Fatalf("revoked reference not removable: %+v %v", view, err)
	}
	view, err = app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1})
	if err != nil || view.Revision != 2 || len(view.Libraries) != 0 {
		t.Fatalf("offline clear failed: %+v %v", view, err)
	}
}

func TestKnowledgeRealLateAccountAndSessionResultsDiscarded(t *testing.T) {
	for _, replace := range []string{"account", "session"} {
		t.Run(replace, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				_, _ = w.Write([]byte(`{"items":[` + knowledgeSummaryJSON(knowledgeTestID) + `],"page":1,"pageSize":10,"total":1,"hasMore":false}`))
			})
			done := make(chan error, 1)
			go func() {
				page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
				if len(page.Items) != 0 {
					t.Errorf("stale data published: %+v", page)
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("real request never started")
			}
			if replace == "account" {
				app.tokens.setInMemory(tokenSet{AccessToken: knowledgeJWT("B"), Expiry: time.Now().Add(time.Hour)})
			} else {
				app.mu.Lock()
				e := app.entryLocked(id)
				app.registerSessionLocked(id, e.workspace, nil, nil, nil)
				app.mu.Unlock()
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("stale result accepted")
			}
		})
	}
}

func TestKnowledgeDirectoryOnlyLaunchesCurrentlySelectedAuthorizedCanonicalURL(t *testing.T) {
	for _, raw := range []string{
		"https://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID + "/data-collection",
		"http://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID + "/data-collection",
		"https://ai.ouchn.edu.cn:443/knowledge-base/" + knowledgeTestID + "/data-collection",
		"https://ai.ouchn.edu.cn@evil.example/knowledge-base/" + knowledgeTestID + "/data-collection",
		"https://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID2 + "/data-collection",
		"https://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID + "/data-collection?token=secret",
		"https://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID + "/data-collection#fragment",
		"https://ai.ouchn.edu.cn/knowledge-base/%31" + knowledgeTestID[1:] + "/data-collection",
		"https://ai.ouchn.edu.cn/knowledge-base/../knowledge-base/" + knowledgeTestID + "/data-collection",
		"https://AI.OUCHN.EDU.CN/knowledge-base/" + knowledgeTestID + "/data-collection",
		"",
	} {
		t.Run(raw, func(t *testing.T) {
			var target atomic.Value
			target.Store("https://ai.ouchn.edu.cn/knowledge-base/" + knowledgeTestID + "/data-collection")
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
				var summary map[string]any
				_ = json.Unmarshal([]byte(knowledgeSummaryJSON(knowledgeTestID)), &summary)
				summary["directoryUrl"] = target.Load().(string)
				_ = json.NewEncoder(w).Encode(summary)
			})
			if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}}); err != nil {
				t.Fatal(err)
			}
			target.Store(raw)
			launched := ""
			err := app.openKnowledgeDirectory(id, knowledgeTestID, func(url string) error { launched = url; return nil })
			if raw == "https://ai.ouchn.edu.cn/knowledge-base/"+knowledgeTestID+"/data-collection" {
				if err != nil || launched != raw {
					t.Fatalf("valid navigation failed: %s %v", launched, err)
				}
			} else if err == nil || launched != "" {
				t.Fatalf("unsafe URL launched: %s %v", launched, err)
			}
			if err := app.openKnowledgeDirectory(id, knowledgeTestID2, func(string) error { t.Error("unselected ID launched"); return nil }); err == nil {
				t.Fatal("unselected ID accepted")
			}
		})
	}
}

func TestKnowledgeDirectoryRechecksSelectionAfterAuthorization(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var delay atomic.Bool
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
		if delay.Load() {
			close(started)
			<-release
		}
		_, _ = w.Write([]byte(knowledgeSummaryJSON(knowledgeTestID)))
	})
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}}); err != nil {
		t.Fatal(err)
	}
	delay.Store(true)
	done := make(chan error, 1)
	go func() {
		done <- app.openKnowledgeDirectory(id, knowledgeTestID, func(string) error { t.Error("removed selection launched"); return nil })
	}()
	<-started
	cleared := make(chan error, 1)
	go func() {
		_, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1})
		cleared <- err
	}()
	select {
	case err := <-cleared:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("directory request held a lifecycle lock")
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("removed reference opened")
	}
}

func TestKnowledgeDirectoryReportsLauncherFailureWithoutRawDetails(t *testing.T) {
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(knowledgeSummaryJSON(knowledgeTestID)))
	})
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}}); err != nil {
		t.Fatal(err)
	}
	err := app.openKnowledgeDirectory(id, knowledgeTestID, func(string) error { return errors.New("secret launcher data") })
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("launcher error unsanitized: %v", err)
	}
}

func TestKnowledgeReal401RefreshRetriesOnlyOnceAndKeepsScope(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "refresh succeeds", true: "second 401"}[rejected], func(t *testing.T) {
			var calls, refreshes atomic.Int32
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 || rejected {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+knowledgeJWT("A") {
					t.Error("refreshed account bearer missing")
				}
				_, _ = w.Write([]byte(`{"items":[],"page":1,"pageSize":10,"total":0,"hasMore":false}`))
			})
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refreshes.Add(1)
				if r.Method != "POST" || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "synthetic-refresh" {
					t.Error("invalid refresh grant")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": knowledgeJWT("A"), "refresh_token": "rotated", "expires_in": 3600})
			}))
			defer tokens.Close()
			app.tokens.mu.Lock()
			app.tokens.ts.RefreshToken = "synthetic-refresh"
			app.tokens.tokenURL = tokens.URL
			app.tokens.mu.Unlock()
			app.mu.Lock()
			scope := knowledgeScope(app.entryLocked(id))
			app.mu.Unlock()
			page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
			if err != nil || calls.Load() != 2 || refreshes.Load() != 1 || (!rejected && page.Mode != "real") || (rejected && page.Mode != "unavailable") {
				t.Fatalf("retry bound/result: %+v %v calls=%d refresh=%d", page, err, calls.Load(), refreshes.Load())
			}
			app.mu.Lock()
			current := knowledgeScope(app.entryLocked(id))
			app.mu.Unlock()
			if current != scope {
				t.Fatal("ordinary refresh changed durable scope")
			}
		})
	}
}

func TestKnowledgeRealRedirectOversizeMalformedAndWrongDetailFailClosed(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		t.Error("redirect forwarded a request")
	}))
	defer other.Close()
	for _, failure := range []string{"redirect", "oversize", "business envelope", "wrong detail"} {
		t.Run(failure, func(t *testing.T) {
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "redirect":
					http.Redirect(w, r, other.URL, http.StatusFound)
				case "oversize":
					_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
				case "business envelope":
					_, _ = w.Write([]byte(`{"code":403,"message":"secret","data":null}`))
				case "wrong detail":
					_, _ = w.Write([]byte(knowledgeSummaryJSON(knowledgeTestID2)))
				}
			})
			if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}}); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("untrusted detail accepted/leaked: %v", err)
			}
			view, err := app.GetSessionKnowledge(id)
			if err != nil || view.Revision != 0 || len(view.Libraries) != 0 {
				t.Fatalf("rejected addition changed durable data: %+v %v", view, err)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("bearer redirect followed")
	}
}

func TestKnowledgeRealRestoresStructurallyValidUnavailableIDsAndSubsetRemoval(t *testing.T) {
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	app.mu.Lock()
	e := app.entryLocked(id)
	app.mu.Unlock()
	disk := knowledgeDisk{Version: 1, SessionID: id, Selections: map[string]knowledgeSelection{knowledgeScope(e): {Revision: 7, LibraryIDs: []string{knowledgeTestID, knowledgeTestID2}}}}
	if err := writeKnowledge(e.workspace, id, disk); err != nil {
		t.Fatal(err)
	}
	view, err := app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 2 || view.Libraries[0].ID != knowledgeTestID || view.Libraries[0].DirectoryURL != "" || view.Libraries[0].UnavailableReason == "" {
		t.Fatalf("restored revoked IDs lost: %+v %v", view, err)
	}
	view, err = app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 7, LibraryIDs: []string{knowledgeTestID2}})
	if err != nil || view.Revision != 8 || len(view.Libraries) != 1 || view.Libraries[0].ID != knowledgeTestID2 {
		t.Fatalf("offline subset removal: %+v %v", view, err)
	}
	// 不可用库仍可普通发送；顶部／状态区提示失权，检索由后续按需工具再验。
	if err := app.SendMessage(id, "ordinary despite unavailable library"); err != nil {
		t.Fatalf("unavailable selection must not block ordinary send: %v", err)
	}
}

func TestKnowledgeRealSubsetAndClearWithExpiredTokenDoNotRefreshOrRequest(t *testing.T) {
	for _, retained := range [][]string{{knowledgeTestID2}, {}} {
		t.Run(map[bool]string{true: "subset", false: "clear"}[len(retained) > 0], func(t *testing.T) {
			var requests atomic.Int32
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			})
			app.mu.Lock()
			e := app.entryLocked(id)
			app.mu.Unlock()
			scope := knowledgeScope(e)
			disk := knowledgeDisk{Version: 1, SessionID: id, Selections: map[string]knowledgeSelection{scope: {Revision: 7, LibraryIDs: []string{knowledgeTestID, knowledgeTestID2}}}}
			if err := writeKnowledge(e.workspace, id, disk); err != nil {
				t.Fatal(err)
			}
			app.tokens.mu.Lock()
			app.tokens.ts.Expiry = time.Now().Add(-time.Hour)
			app.tokens.ts.RefreshToken = ""
			epoch := app.tokens.identity
			app.tokens.mu.Unlock()
			view, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 7, LibraryIDs: retained})
			if err != nil || view.Revision != 8 || len(view.Libraries) != len(retained) {
				t.Fatalf("expired-token removal failed: %+v %v", view, err)
			}
			if len(retained) > 0 && (view.Libraries[0].ID != knowledgeTestID2 || view.Libraries[0].DirectoryURL != "" || view.Libraries[0].UnavailableReason == "") {
				t.Fatalf("unvalidated retained reference appears authorized: %+v", view)
			}
			stored, err := readKnowledge(e.workspace, id)
			if err != nil || stored.Selections[scope].Revision != 8 || len(stored.Selections[scope].LibraryIDs) != len(retained) {
				t.Fatalf("removal was not durable: %+v %v", stored, err)
			}
			app.tokens.mu.Lock()
			unchanged := app.tokens.identity == epoch && app.tokens.ts.AccessToken == knowledgeJWT("A")
			app.tokens.mu.Unlock()
			if !unchanged || requests.Load() != 0 {
				t.Fatalf("removal touched authentication/network: unchanged=%v requests=%d", unchanged, requests.Load())
			}
		})
	}
}

func TestKnowledgeDemoRequiresOptInAndRealScopeIsIsolated(t *testing.T) {
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"page":1,"pageSize":10,"total":0,"hasMore":false}`))
	})
	previous := testBuild
	testBuild = "test"
	t.Cleanup(func() { testBuild = previous })
	page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
	if err != nil || page.Mode != "real" {
		t.Fatalf("test build silently enabled demo: %+v %v", page, err)
	}
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "demo")
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "")
	view, err := app.GetSessionKnowledge(id)
	if err != nil || view.Mode != "real" || len(view.Libraries) != 0 || view.Revision != 0 {
		t.Fatalf("demo leaked into real scope: %+v %v", view, err)
	}
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "demo")
	view, err = app.GetSessionKnowledge(id)
	if err != nil || view.Mode != "demo" || len(view.Libraries) != 1 || !view.Libraries[0].DocumentCountKnown {
		t.Fatalf("demo state not restored: %+v %v", view, err)
	}
	testBuild = ""
	page, err = app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
	if err != nil || page.Mode != "real" {
		t.Fatalf("production accepted demo opt-in: %+v %v", page, err)
	}
}

func TestKnowledgeRealAdditionDiscardedOnAccountOrEntryReplacement(t *testing.T) {
	for _, replacement := range []string{"account", "tenant/bridge/session"} {
		t.Run(replacement, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
				if once.CompareAndSwap(false, true) {
					close(started)
					<-release
				}
				_, _ = w.Write([]byte(knowledgeSummaryJSON(knowledgeTestID)))
			})
			app.mu.Lock()
			original := app.entryLocked(id)
			app.mu.Unlock()
			done := make(chan error, 1)
			go func() {
				_, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}})
				done <- err
			}()
			<-started
			if replacement == "account" {
				app.tokens.setInMemory(tokenSet{AccessToken: knowledgeJWT("B"), Expiry: time.Now().Add(time.Hour)})
			} else {
				app.mu.Lock()
				app.registerSessionLocked(id, original.workspace, nil, nil, nil)
				e := app.entryLocked(id)
				e.tenantID = "tenant-B"
				app.bindKnowledgeLocked(e)
				app.mu.Unlock()
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("late addition accepted")
			}
			if _, err := os.Stat(filepath.Join(original.workspace, ".runcode", "knowledge", id+".json")); !os.IsNotExist(err) {
				t.Fatalf("stale addition persisted: %v", err)
			}
		})
	}
}

func TestKnowledgeRealWholeCatalogTimeoutIncludesSlowBody(t *testing.T) {
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	start := time.Now()
	page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
	if time.Since(start) > 12*time.Second || err != nil || page.Mode != "unavailable" || len(page.Items) != 0 {
		t.Fatalf("whole request deadline missing: %+v %v elapsed=%s", page, err, time.Since(start))
	}
}

func TestKnowledgeRealTokenRefreshWaitHonorsRequestContext(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	app, id := realKnowledgeApp(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("expired token reached catalog") })
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": knowledgeJWT("A"), "expires_in": 3600})
	}))
	defer tokens.Close()
	app.tokens.mu.Lock()
	app.tokens.ts.Expiry = time.Now()
	app.tokens.ts.RefreshToken = "synthetic-refresh"
	app.tokens.tokenURL = tokens.URL
	app.tokens.mu.Unlock()
	app.mu.Lock()
	e := app.entryLocked(id)
	app.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { var target any; done <- app.knowledgeGET(ctx, e, "", &target) }()
	<-started
	select {
	case err := <-done:
		if err == nil {
			t.Error("timed-out refresh succeeded")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("refresh blocked request deadline")
	}
	close(release)
	// Let the shared token manager complete its serialized refresh before test
	// cleanup restores environment/config paths.
	app.tokens.mu.Lock()
	if app.tokens.ts.AccessToken != knowledgeJWT("A") || time.Until(app.tokens.ts.Expiry) < time.Hour-time.Minute {
		t.Error("serialized refresh did not finish after the knowledge waiter timed out")
	}
	app.tokens.mu.Unlock()
	// Token() persists after releasing its mutex. Wait for that actual write before
	// restoring this test's environment; the mutex alone is not a completion fence.
	tokenPath, err := passportTokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, supported := protectSecret("synthetic persistence probe"); supported {
		deadline := time.Now().Add(3 * time.Second)
		for {
			if info, err := os.Stat(tokenPath); err == nil && info.Size() > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("refresh persistence did not complete inside isolated config")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
