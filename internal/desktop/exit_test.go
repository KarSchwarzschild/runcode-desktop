package desktop

import (
	"testing"
	"time"
)

func TestPrepareExitRunsOnceAndSharesDeadline(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	if !app.PrepareExit(time.Second) {
		t.Fatal("idle cleanup did not finish")
	}
	if !app.PrepareExit(time.Second) {
		t.Fatal("second exit path did not see the finished cleanup")
	}
}

// A build holding startMu must not keep the process alive past the deadline,
// and a second exit path must not be granted a fresh wait.
func TestPrepareExitIsBoundedWhenCleanupBlocks(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	app.startMu.Lock()
	defer app.startMu.Unlock()
	start := time.Now()
	if app.PrepareExit(200 * time.Millisecond) {
		t.Fatal("cleanup reported done while startMu was held")
	}
	if app.PrepareExit(10 * time.Second) {
		t.Fatal("cleanup reported done while startMu was held")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("second exit path waited again: %s", elapsed)
	}
}
