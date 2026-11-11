package desktop

import (
	"context"
	"testing"
)

func TestKnowledgeSelectionRestartRestore(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01", "demo:policies"}, ExpectedRevision: 0})
	if err != nil || selected.Revision != 1 || len(selected.Libraries) != 2 {
		t.Fatalf("save: %+v %v", selected, err)
	}
	if err := app.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newParallelApp(t)
	defer func() { _ = restarted.mgr.CloseAll(context.Background()) }()
	restarted.mu.Lock()
	restarted.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	restarted.mu.Unlock()
	snap, err := restarted.GetSessionKnowledgeSnapshot(id)
	if err != nil || snap.Revision != 1 || len(snap.Libraries) != 2 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
	for _, library := range snap.Libraries {
		if library.Title == "" || library.Title == "名称待恢复" {
			t.Fatalf("expected persisted title, got %+v", library)
		}
	}
}

func TestKnowledgeSelectionClearPersists(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}, ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	cleared, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1})
	if err != nil || cleared.Revision != 2 || len(cleared.Libraries) != 0 {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	if err := app.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newParallelApp(t)
	defer func() { _ = restarted.mgr.CloseAll(context.Background()) }()
	restarted.mu.Lock()
	restarted.registerSessionLocked(id, sess.cwd, nil, nil, nil)
	restarted.mu.Unlock()
	snap, err := restarted.GetSessionKnowledgeSnapshot(id)
	if err != nil || snap.Revision != 2 || len(snap.Libraries) != 0 {
		t.Fatalf("cleared selection revived: %+v %v", snap, err)
	}
}

func TestKnowledgeSelectionSnapshotV1Compatibility(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, _ := openParallelSession(t, app, built)
	e, err := app.entryOf(id)
	if err != nil {
		t.Fatal(err)
	}
	disk := knowledgeDisk{
		Version:   knowledgeStoreVersionV1,
		SessionID: id,
		Selections: map[string]knowledgeSelection{
			knowledgeScope(e): {Revision: 3, LibraryIDs: []string{"demo:employee-01"}},
		},
	}
	if err := writeKnowledge(e.workspace, id, disk); err != nil {
		t.Fatal(err)
	}
	snap, err := app.GetSessionKnowledgeSnapshot(id)
	if err != nil || snap.Revision != 3 || len(snap.Libraries) != 1 || snap.Libraries[0].ID != "demo:employee-01" || snap.Libraries[0].Title != "名称待恢复" {
		t.Fatalf("v1 snapshot: %+v %v", snap, err)
	}
}
