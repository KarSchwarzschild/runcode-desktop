package desktop

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wt68/runcode/internal/automation"
	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
)

type exitAutomationRunner struct {
	starts atomic.Int32
	cancel atomic.Int32
}

func (r *exitAutomationRunner) Start(context.Context, automation.Task, automation.Run) (automation.Submission, error) {
	r.starts.Add(1)
	return automation.Submission{SessionID: "automation-session", QuestionID: "automation-question"}, nil
}

func (r *exitAutomationRunner) Cancel(context.Context, automation.Run) error {
	r.cancel.Add(1)
	return nil
}

func TestPrepareExitStopsAutomationBeforeSessionDrain(t *testing.T) {
	isolateConfigDir(t)
	app := newWithBuild(&recordingSink{}, func(_ engine.Config, _ engine.Options) (host.Session, error) {
		return newFakeHostSession(), nil
	})

	store, err := automation.OpenStore(filepath.Join(t.TempDir(), "automation.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	runner := &exitAutomationRunner{}
	svc := automation.NewService(store, runner, automation.SystemClock{})
	app.bindAutomation(svc, store)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Stop(); _ = store.Close() })

	now := time.Now().UTC()
	first := dueAutomationTask(now, "active", t.TempDir(), automation.Binding{ModelName: "m"})
	if _, err := store.CreateTask(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	svc.Wake()
	deadline := time.Now().Add(2 * time.Second)
	for runner.starts.Load() == 0 && time.Now().Before(deadline) {
		svc.Wake()
		time.Sleep(10 * time.Millisecond)
	}
	if runner.starts.Load() != 1 {
		t.Fatalf("initial starts = %d", runner.starts.Load())
	}
	late := dueAutomationTask(time.Now().UTC(), "late", t.TempDir(), automation.Binding{ModelName: "m"})
	if _, err := store.CreateTask(context.Background(), late); err != nil {
		t.Fatal(err)
	}
	svc.Wake()
	time.Sleep(50 * time.Millisecond)
	if runner.starts.Load() != 1 {
		t.Fatalf("the busy queue started a second run: %d", runner.starts.Load())
	}

	done := make(chan bool, 1)
	go func() { done <- app.PrepareExit(time.Second) }()
	deadline = time.Now().Add(2 * time.Second)
	for runner.cancel.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runner.cancel.Load() < 1 {
		t.Fatalf("active automation was not cancelled before session drain: %d", runner.cancel.Load())
	}

	if runner.starts.Load() != 1 {
		t.Fatalf("a new run was admitted during exit: %d", runner.starts.Load())
	}

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("exit did not finish")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exit did not finish")
	}
}
