package desktop

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKnowledgePendingAdmissionLifecycleCancelsHTTP(t *testing.T) {
	for _, operation := range []string{"close", "logout", "close-all", "exit"} {
		t.Run(operation, func(t *testing.T) {
			var block atomic.Bool
			entered, cancelled := make(chan struct{}), make(chan struct{})
			app, id := realKnowledgeApp(t, func(w http.ResponseWriter, r *http.Request) {
				if block.Swap(false) {
					close(entered)
					<-r.Context().Done()
					close(cancelled)
					return
				}
				if r.Method != http.MethodGet {
					t.Error("cancelled admission reached retrieval")
				}
				fmt.Fprint(w, knowledgeSummaryJSON(knowledgeTestID))
			})
			app.mu.Lock()
			e := app.entryLocked(id)
			e.passport = true
			e.connectionConfig.BaseURL = passportConfig().BridgeBaseURL + "/t/tenant-A/v1"
			app.mu.Unlock()
			block.Store(true)
			submitted := make(chan error, 1)
			go func() {
				// Exercise cancellation of the existing admission mechanism without
				// re-enabling the retired engine knowledge pipeline.
				app.startMu.Lock()
				defer app.startMu.Unlock()
				ctx, cancel := context.WithCancel(context.Background())
				finish := app.beginQuestionAdmission(ctx, e, cancel)
				defer finish()
				_, err := app.knowledgeDetail(ctx, e, knowledgeTestID)
				submitted <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("detail did not start")
			}
			stopped := make(chan error, 1)
			go func() {
				switch operation {
				case "close":
					stopped <- app.CloseSession(id)
				case "close-all":
					stopped <- app.CloseAllSessions()
				case "exit":
					if !app.PrepareExit(time.Second) {
						stopped <- fmt.Errorf("exit waited behind pending admission")
						return
					}
					stopped <- nil
				default:
					app.PassportLogout()
					stopped <- nil
				}
			}()
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("lifecycle operation blocked behind pre-admission network")
			}
			select {
			case err := <-submitted:
				if err == nil {
					t.Fatal("cancelled submission accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("pending validation did not cancel")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("HTTP context not cancelled")
			}
			raw, err := os.ReadFile(filepath.Join(e.workspace, ".runcode", "sessions", id+".jsonl"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "pending input must remain unsent") {
				t.Fatal("cancelled validation persisted user input")
			}
			app.mu.Lock()
			pending := e.pendingQuestion
			lastText := e.lastUserText
			app.mu.Unlock()
			if operation != "logout" && len(app.mgr.List()) != 0 {
				t.Fatal("lifecycle operation left an open session")
			}
			if pending != nil || lastText != "" {
				t.Fatal("cancelled admission retained bookkeeping")
			}
		})
	}
}

func TestQuestionAdmissionCleanupCannotRemoveNewAttempt(t *testing.T) {
	app := &App{}
	entry := &sessionEntry{}
	oldCtx, oldCancel := context.WithCancel(context.Background())
	finishOld := app.beginQuestionAdmission(oldCtx, entry, oldCancel)
	newCtx, newCancel := context.WithCancel(context.Background())
	finishNew := app.beginQuestionAdmission(newCtx, entry, newCancel)
	defer finishNew()
	finishOld()
	if oldCtx.Err() == nil || newCtx.Err() != nil {
		t.Fatal("old cleanup cancelled the wrong attempt")
	}
	app.mu.Lock()
	current := entry.pendingQuestion
	app.mu.Unlock()
	if current == nil || current.ctx != newCtx {
		t.Fatal("old cleanup removed the newer admission")
	}
}
