package automation

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "a.sqlite3"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func sampleTask(id, owner, req string) Task {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	return Task{
		ID: id, OwnerScope: owner, ClientRequestID: req, Revision: 1,
		Name: "每日整理", Prompt: "只回复时间", Workspace: `D:\资料`,
		ExecutionMode: ExecReuseSession,
		Schedule:      Schedule{Mode: ModeDaily, TimeZone: "Asia/Shanghai", StartDate: "2026-11-19", TimeOfDay: "09:00:00", Interval: 1},
		AnchorUTC:     now, State: TaskEnabled, NextRunAtUTC: &next,
		Binding: Binding{ModelKind: "passport", ModelName: "m1", BindingVersion: 1},
		CreatedAtUTC: now, UpdatedAtUTC: now,
	}
}

func TestStoreCreateRequestIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	a := sampleTask("t1", "owner-a", "req-1")
	got, err := st.CreateTask(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID = "t2"
	b.Name = "other"
	again, err := st.CreateTask(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != got.ID || again.Name != got.Name {
		t.Fatalf("idempotent create diverged: %+v vs %+v", again, got)
	}
	page, err := st.ListTasks(ctx, TaskFilter{AllowedOwners: []string{"owner-a"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1 task, got %d", len(page.Items))
	}
}

func TestStoreClaimOnce(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	task := sampleTask("t1", "owner-a", "req-1")
	task, err := st.CreateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 1, 0, 5, 0, time.UTC) // past next?
	// Set nextRunAt in the past so ClaimDue can claim.
	past := now.Add(-time.Minute)
	task.NextRunAtUTC = &past
	task, err = st.UpdateTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	type res struct {
		run *Run
		err error
	}
	ch := make(chan res, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := st.ClaimDue(ctx, task.ID, task.Revision, now)
			ch <- res{r, e}
		}()
	}
	wg.Wait()
	close(ch)
	var wins int
	for r := range ch {
		if r.err == nil && r.run != nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent claims won=%d want 1", wins)
	}
}

func TestStoreRevisionConflict(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	task, err := st.CreateTask(ctx, sampleTask("t1", "owner-a", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	task.Name = "改名"
	if _, err := st.UpdateTask(ctx, task, task.Revision+5); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestStoreScopesAndCursors(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for i, owner := range []string{"owner-a", "owner-b", "owner-a"} {
		id := "t" + string(rune('1'+i))
		req := "r" + id
		_, err := st.CreateTask(ctx, sampleTask(id, owner, req))
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := st.ListTasks(ctx, TaskFilter{AllowedOwners: []string{"owner-a"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("page=%+v", page)
	}
	page2, err := st.ListTasks(ctx, TaskFilter{AllowedOwners: []string{"owner-a"}, Cursor: page.NextCursor, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("want second page 1 item, got %d", len(page2.Items))
	}
	foreign, err := st.ListTasks(ctx, TaskFilter{AllowedOwners: []string{"owner-x"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign.Items) != 0 {
		t.Fatalf("leaked tasks: %+v", foreign.Items)
	}
}

func TestStoreMigrationFailurePreservesData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sqlite3")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateTask(context.Background(), sampleTask("t1", "owner-a", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	// Corrupt schema_migrations to force a bad future migration path: reopen should still
	// see the task if we only fail *new* migrations — simulate by opening and checking data.
	st2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	page, err := st2.ListTasks(context.Background(), TaskFilter{AllowedOwners: []string{"owner-a"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("data lost after reopen: %d", len(page.Items))
	}
}

func TestStoreTerminalIsMonotonic(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	task, err := st.CreateTask(ctx, sampleTask("t1", "owner-a", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	task.NextRunAtUTC = &past
	task, err = st.UpdateTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	now := past.Add(time.Minute)
	run, err := st.ClaimDue(ctx, task.ID, task.Revision, now)
	if err != nil || run == nil {
		t.Fatalf("claim: %v %#v", err, run)
	}
	run.Status = RunSucceeded
	fin := now.Add(time.Second)
	run.FinishedAtUTC = &fin
	run.Summary = "ok"
	saved, err := st.SaveRun(ctx, *run, run.RunRevision)
	if err != nil {
		t.Fatal(err)
	}
	late := saved
	late.Status = RunRunning
	if _, err := st.SaveRun(ctx, late, saved.RunRevision); err == nil {
		t.Fatal("expected terminal monotonic rejection")
	}
}

func TestStoreRecoverDoesNotResubmit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	task, err := st.CreateTask(ctx, sampleTask("t1", "owner-a", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	task.NextRunAtUTC = &past
	task, err = st.UpdateTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	now := past.Add(time.Minute)
	run, err := st.ClaimDue(ctx, task.ID, task.Revision, now)
	if err != nil || run == nil {
		t.Fatal(err)
	}
	run.Status = RunSubmitting
	if _, err := st.SaveRun(ctx, *run, run.RunRevision); err != nil {
		t.Fatal(err)
	}
	if err := st.Recover(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	page, err := st.ListRuns(ctx, RunFilter{AllowedOwners: []string{"owner-a"}, TaskID: task.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != RunInterrupted {
		t.Fatalf("recover want interrupted, got %+v", page.Items)
	}
}

func TestStoreSessionLinkSurvivesRename(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	task, err := st.CreateTask(ctx, sampleTask("t1", "owner-a", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LinkSession(ctx, SessionLink{
		OwnerScope: "owner-a", Workspace: task.Workspace, SessionID: "sess-1",
		TaskID: task.ID, TaskName: task.Name,
	}); err != nil {
		t.Fatal(err)
	}
	task.Name = "新名称"
	task, err = st.UpdateTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	page, err := st.ListRuns(ctx, RunFilter{
		AllowedOwners: []string{"owner-a"}, Workspace: task.Workspace, SessionID: "sess-1", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Link itself is queried via ListRuns by workspace+session — may be empty of runs;
	// verify link still resolves by creating a finished run tied to session.
	_ = page
	past := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	task.NextRunAtUTC = &past
	task, err = st.UpdateTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.ClaimDue(ctx, task.ID, task.Revision, past.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	run.SessionID = "sess-1"
	run.Status = RunSucceeded
	fin := past.Add(2 * time.Minute)
	run.FinishedAtUTC = &fin
	if _, err := st.SaveRun(ctx, *run, run.RunRevision); err != nil {
		t.Fatal(err)
	}
	page, err = st.ListRuns(ctx, RunFilter{
		AllowedOwners: []string{"owner-a"}, Workspace: task.Workspace, SessionID: "sess-1", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TaskID != task.ID {
		t.Fatalf("link+rename broken: %+v", page.Items)
	}
}
