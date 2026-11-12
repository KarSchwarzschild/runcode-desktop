package desktop

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func enableKnowledgeDemo(t *testing.T) {
	t.Helper()
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "demo")
	previous := testBuild
	testBuild = "knowledge-test"
	t.Cleanup(func() { testBuild = previous })
}

func TestKnowledgeCatalogExplicitDemoOnly(t *testing.T) {
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, _ := openParallelSession(t, app, built)
	previous := testBuild
	testBuild = ""
	t.Cleanup(func() { testBuild = previous })
	page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
	if err != nil || page.Mode != "unavailable" || page.UnavailableReason == "" || len(page.Items) != 0 {
		t.Fatalf("normal build fabricated catalog: %+v %v", page, err)
	}
	testBuild = "test"
	t.Setenv("RUNCODE_KNOWLEDGE_MODE", "demo")
	page, err = app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id, Page: 1, PageSize: 2})
	if err != nil || page.Mode != "demo" || page.Total != 12 || len(page.Items) != 2 || page.MaxSelection != 5 {
		t.Fatalf("demo page: %+v %v", page, err)
	}
	for _, item := range page.Items {
		if !strings.HasPrefix(item.ID, "demo:") || item.Source != "demo" || item.Description == "" || !strings.HasPrefix(item.Path, "测试目录 / ") || item.DocumentCount < 1 {
			t.Fatalf("unsafe/incomplete demo: %+v", item)
		}
	}
	filtered, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id, Query: "数字员工"})
	if err != nil || filtered.Total != 1 || filtered.Items[0].Title != "数字员工01（测试示例）" {
		t.Fatalf("search: %+v %v", filtered, err)
	}
	if _, err := app.ListKnowledgeBases(KnowledgeListRequest{}); err == nil {
		t.Fatal("accepted implicit focused session")
	}
}

func TestKnowledgeSelectionRevisionValidationIsolationAndRestore(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	idA, sessA := openParallelSession(t, app, built)
	idB, _ := openParallelSession(t, app, built)
	initial, err := app.GetSessionKnowledge(idA)
	if err != nil || initial.Revision != 0 || len(initial.Libraries) != 0 {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: idA, LibraryIDs: []string{"demo:employee-01", "demo:policies", "demo:employee-01"}, ExpectedRevision: 0})
	if err != nil || selected.Revision != 1 || len(selected.Libraries) != 2 {
		t.Fatalf("set: %+v %v", selected, err)
	}
	same, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: idA, LibraryIDs: []string{"demo:policies", "demo:employee-01"}, ExpectedRevision: 1})
	if err != nil || same.Revision != 1 {
		t.Fatalf("same set was not idempotent: %+v %v", same, err)
	}
	for _, tc := range []struct {
		ids []string
		rev int64
	}{
		{nil, 0}, {[]string{"demo:unknown"}, 1}, {[]string{"demo:employee-01", "demo:policies", "demo:products", "demo:onboarding", "demo:it-support", "demo:finance"}, 1},
	} {
		if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: idA, LibraryIDs: tc.ids, ExpectedRevision: tc.rev}); err == nil {
			t.Fatalf("accepted invalid/stale selection: %+v", tc)
		}
	}
	other, err := app.GetSessionKnowledge(idB)
	if err != nil || other.Revision != 0 || len(other.Libraries) != 0 {
		t.Fatalf("A leaked into B: %+v %v", other, err)
	}
	// Simulate close/reopen and a new App without copying any in-memory state.
	if err := app.CloseSession(idA); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newParallelApp(t)
	defer func() { _ = restarted.mgr.CloseAll(context.Background()) }()
	restarted.mu.Lock()
	restarted.registerSessionLocked(idA, sessA.cwd, nil, nil, nil)
	restarted.mu.Unlock()
	restored, err := restarted.GetSessionKnowledge(idA)
	if err != nil || !reflect.DeepEqual(selected, restored) {
		t.Fatalf("restart lost selection: %+v %v", restored, err)
	}
	cleared, err := restarted.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: idA, ExpectedRevision: 1})
	if err != nil || cleared.Revision != 2 || len(cleared.Libraries) != 0 {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
}

func TestKnowledgeCatalogPagesTenThenTwoWithoutDuplicates(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, _ := openParallelSession(t, app, built)
	seen := make(map[string]bool)
	for pageNumber, wantCount := range []int{10, 2, 0} {
		page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id, Page: pageNumber + 1, PageSize: 10})
		if err != nil || page.Total != 12 || page.Page != pageNumber+1 || page.PageSize != 10 || len(page.Items) != wantCount {
			t.Fatalf("page %d: %+v %v", pageNumber+1, page, err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate library across pages: %s", item.ID)
			}
			seen[item.ID] = true
			if item.Source != "demo" || !strings.HasSuffix(item.Title, "（测试示例）") || !strings.HasPrefix(item.Path, "测试目录 / ") || item.Description == "" || item.DocumentCount < 1 {
				t.Fatalf("incomplete or unmarked test entry: %+v", item)
			}
		}
	}
	if len(seen) != 12 {
		t.Fatalf("pagination omitted entries: %d", len(seen))
	}
}

func TestKnowledgeFiveSelectionsRestoreAndSixthLeavesStateUnchanged(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	ids := []string{"demo:employee-01", "demo:policies", "demo:products", "demo:onboarding", "demo:it-support"}
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: ids})
	if err != nil || selected.Revision != 1 || len(selected.Libraries) != 5 {
		t.Fatalf("five selections: %+v %v", selected, err)
	}
	path := filepath.Join(sess.cwd, ".runcode", "knowledge", id+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: append(ids, "demo:finance"), ExpectedRevision: 1}); err == nil || !strings.Contains(err.Error(), "5") {
		t.Fatalf("six selections must be rejected with current limit: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected sixth library changed durable selection")
	}
	if err := app.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newParallelApp(t)
	defer func() { _ = restarted.mgr.CloseAll(context.Background()) }()
	restarted.mu.Lock()
	restarted.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	restarted.mu.Unlock()
	restored, err := restarted.GetSessionKnowledge(id)
	if err != nil || !reflect.DeepEqual(selected, restored) {
		t.Fatalf("five selections failed restart restore: %+v %v", restored, err)
	}
	// 重启后仅登记外壳 entry 时 Manager 尚无会话；同会话发送由 TestKnowledgeSelectedAllowsOrdinaryAgentTurn 覆盖。
}

func TestKnowledgeWriteFailureCannotPublishSelection(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	// A file where the sidecar directory should be makes real disk IO fail.
	if err := os.MkdirAll(filepath.Join(sess.cwd, ".runcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sess.cwd, ".runcode", "knowledge"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err == nil {
		t.Fatal("reported successful selection despite failed storage")
	}
	if err := app.SendMessage(id, "must not execute after storage failure"); err == nil {
		t.Fatal("storage read failure reached model")
	}
}

func TestKnowledgeAccountEpochIsolationAndStableRestore(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	setAccount := func(sub string) {
		app.tokens.setInMemory(tokenSet{AccessToken: "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"test","sub":"`+sub+`"}`)) + ".x", Expiry: time.Now().Add(time.Hour)})
	}
	setAccount("A")
	id, sess := openParallelSession(t, app, built)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err != nil {
		t.Fatal(err)
	}
	setAccount("B")
	if _, err := app.GetSessionKnowledge(id); err == nil {
		t.Fatal("old account entry remained usable")
	}
	if err := app.SendMessage(id, "stale account"); err == nil {
		t.Fatal("old account input reached model")
	}
	// Same history under B receives its own empty selection, never A's.
	app.mu.Lock()
	app.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	app.mu.Unlock()
	view, err := app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 0 {
		t.Fatalf("cross account leak: %+v %v", view, err)
	}
	setAccount("A")
	app.mu.Lock()
	app.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	app.mu.Unlock()
	view, err = app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 1 {
		t.Fatalf("same account selection not restored: %+v %v", view, err)
	}
}

func TestKnowledgeTenantAndBridgeScopesDoNotLeakAcrossReopenedEntries(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	reopen := func(tenant, bridge string) {
		t.Setenv("RUNCODE_BRIDGE_BASE_URL", bridge)
		app.mu.Lock()
		defer app.mu.Unlock()
		app.registerSessionLocked(id, sess.cwd, nil, nil, nil)
		e := app.entryLocked(id)
		e.tenantID, e.connectionConfig.BaseURL = tenant, bridge
		app.bindKnowledgeLocked(e)
	}
	reopen("tenant-A", "https://synthetic-a.invalid/v1")
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct{ tenant, bridge string }{{"tenant-B", "https://synthetic-a.invalid/v1"}, {"tenant-A", "https://synthetic-b.invalid/v1"}} {
		reopen(scope.tenant, scope.bridge)
		view, err := app.GetSessionKnowledge(id)
		if err != nil || len(view.Libraries) != 0 {
			t.Fatalf("selection leaked across scope %+v: %+v %v", scope, view, err)
		}
	}
	reopen("tenant-A", "https://synthetic-a.invalid/v1")
	view, err := app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 1 || view.Revision != 1 {
		t.Fatalf("same scope did not restore: %+v %v", view, err)
	}
}

func TestKnowledgeEmptyCustomSessionSurvivesPassportAccountChange(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	app.tokens.setInMemory(tokenSet{AccessToken: "opaque-new-passport-login", Expiry: time.Now().Add(time.Hour)})
	view, err := app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 0 {
		t.Fatalf("empty custom session blocked by unrelated Passport login: %+v %v", view, err)
	}
	if err := app.SendMessage(id, "custom connection keeps ordinary chat"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-sess.started:
		if text != "custom connection keeps ordinary chat" {
			t.Fatal(text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary custom chat never started")
	}
}

func TestKnowledgeDeleteSessionCleansEveryAccountSelection(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err != nil {
		t.Fatal(err)
	}
	if err := app.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteSession(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sess.cwd, ".runcode", "knowledge", id+".json")); !os.IsNotExist(err) {
		t.Fatalf("selection survived deletion: %v", err)
	}
	app.mu.Lock()
	app.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	app.mu.Unlock()
	view, err := app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 0 || view.Revision != 0 {
		t.Fatalf("deleted selection restored: %+v %v", view, err)
	}
}

func TestKnowledgeConcurrentSavesCompareRevisionAtomically(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, _ := openParallelSession(t, app, built)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, library := range []string{"demo:employee-01", "demo:policies"} {
		wg.Add(1)
		go func(idToSelect string) {
			defer wg.Done()
			<-start
			_, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{idToSelect}, ExpectedRevision: 0})
			errors <- err
		}(library)
	}
	close(start)
	wg.Wait()
	close(errors)
	succeeded, failed := 0, 0
	for err := range errors {
		if err == nil {
			succeeded++
		} else {
			failed++
		}
	}
	if succeeded != 1 || failed != 1 {
		t.Fatalf("concurrent CAS: success=%d rejected=%d", succeeded, failed)
	}
	view, err := app.GetSessionKnowledge(id)
	if err != nil || view.Revision != 1 || len(view.Libraries) != 1 {
		t.Fatalf("concurrent save lost atomicity: %+v %v", view, err)
	}
}

func TestKnowledgeStoreRejectsMalformedSchema(t *testing.T) {
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	dir := filepath.Join(sess.cwd, ".runcode", "knowledge")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		`{}`, `{"version":3,"sessionId":"` + id + `","selections":{}}`,
		`{"version":1,"sessionId":"wrong","selections":{}}`,
		`{"version":1,"sessionId":"` + id + `","selections":null}`,
		`{"version":1,"sessionId":"` + id + `","selections":{"short":{"revision":1,"libraryIds":[]}}}`,
		strings.Repeat("x", 65537),
	} {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := app.GetSessionKnowledge(id); err == nil {
			t.Fatalf("malformed sidecar read as empty selection: %.80s", data)
		}
		if err := app.SendMessage(id, "must not send malformed selection"); err == nil {
			t.Fatalf("malformed sidecar reached model: %.80s", data)
		}
	}
}

func TestKnowledgeStoreCannotEscapeWorkspace(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sess.cwd, ".runcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(sess.cwd, ".runcode", "knowledge")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err == nil {
		t.Fatal("knowledge sidecar escaped workspace")
	}
	if err := app.SendMessage(id, "must not execute via external storage"); err == nil {
		t.Fatal("external sidecar treated as empty selection")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("knowledge save wrote outside workspace")
	}
}
