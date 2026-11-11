package desktop

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSetKnowledgeReusesAddedDetailWithinOperation 验证同一次保存内新增库详情只读一次。
func TestSetKnowledgeReusesAddedDetailWithinOperation(t *testing.T) {
	var detailHits atomic.Int32
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/t/tenant-A/v1/knowledge-bases/") && r.Method == http.MethodGet {
			detailHits.Add(1)
			libID := strings.TrimPrefix(r.URL.Path, "/t/tenant-A/v1/knowledge-bases/")
			_, _ = w.Write([]byte(knowledgeSummaryJSON(libID)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	view, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID}})
	if err != nil || view.Revision != 1 || len(view.Libraries) != 1 || view.Libraries[0].Title != "Authorized library" {
		t.Fatalf("first add: %+v %v", view, err)
	}
	if detailHits.Load() != 1 {
		t.Fatalf("expected one detail read on first add, got %d", detailHits.Load())
	}
	view, err = app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1, LibraryIDs: []string{knowledgeTestID, knowledgeTestID2}})
	if err != nil || view.Revision != 2 || len(view.Libraries) != 2 {
		t.Fatalf("second add: %+v %v", view, err)
	}
	// 保留库再读一次 + 新增库读一次 = 共 +2；新增库不得在同次操作内被读两次。
	if detailHits.Load() != 3 {
		t.Fatalf("expected three detail reads total (1+1 retained +1 added), got %d", detailHits.Load())
	}
	view, err = app.GetSessionKnowledge(id)
	if err != nil || len(view.Libraries) != 2 {
		t.Fatalf("independent get: %+v %v", view, err)
	}
	if detailHits.Load() != 5 {
		t.Fatalf("cross-request get must revalidate both IDs; hits=%d", detailHits.Load())
	}
}

func TestRemovalOnlyKeepsOfflineBehavior(t *testing.T) {
	var detailHits atomic.Int32
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/t/tenant-A/v1/knowledge-bases/") {
			detailHits.Add(1)
			libID := strings.TrimPrefix(r.URL.Path, "/t/tenant-A/v1/knowledge-bases/")
			_, _ = w.Write([]byte(knowledgeSummaryJSON(libID)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{knowledgeTestID, knowledgeTestID2}}); err != nil {
		t.Fatal(err)
	}
	before := detailHits.Load()
	view, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1, LibraryIDs: []string{knowledgeTestID}})
	if err != nil || view.Revision != 2 || len(view.Libraries) != 1 || view.Libraries[0].ID != knowledgeTestID {
		t.Fatalf("offline removal: %+v %v", view, err)
	}
	if detailHits.Load() != before {
		t.Fatalf("pure removal must not call detail; before=%d after=%d", before, detailHits.Load())
	}
}
