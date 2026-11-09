package desktop

import (
	"sync"
	"time"
)

// ExitTimeout bounds the whole exit cleanup. Each step has its own limit
// (recorder stopTimeout, sessionCloseTimeout), but CloseAllSessions first waits
// for startMu, and a session build stuck on a slow MCP server or Bridge would
// otherwise hold the process — and its single-instance lock — forever.
const ExitTimeout = 20 * time.Second

// exitState runs the exit cleanup at most once; every exit path (close button,
// in-app quit, SIGTERM, update install) shares the same result.
type exitState struct {
	once     sync.Once
	done     chan struct{}
	deadline time.Time
}

// PrepareExit stops the recorder and closes every session, waiting at most
// limit. It reports whether the cleanup finished. Later calls, even from
// another exit path, wait on the same run and the same deadline instead of
// starting a second run or granting it a fresh limit.
//
// The recorder and the sessions are independent, so they stop concurrently:
// serially their two 15 s limits would add up.
func (a *App) PrepareExit(limit time.Duration) bool {
	a.exit.once.Do(func() {
		a.exit.done = make(chan struct{})
		a.exit.deadline = time.Now().Add(limit)
		go func() {
			defer close(a.exit.done)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.Shutdown()
			}()
			if err := a.CloseAllSessions(); err != nil {
				debugLog("exit: close sessions: %v", err)
			}
			wg.Wait()
		}()
	})
	timer := time.NewTimer(time.Until(a.exit.deadline))
	defer timer.Stop()
	select {
	case <-a.exit.done:
		return true
	case <-timer.C:
		debugLog("exit: cleanup still running after %s; exiting anyway", limit)
		return false
	}
}
