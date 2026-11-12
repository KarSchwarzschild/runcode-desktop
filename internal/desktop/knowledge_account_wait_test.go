package desktop

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKnowledgeAccountForRequestWaitsUnderContext(t *testing.T) {
	app := &App{
		sessions: map[string]*sessionEntry{},
		tokens:   &tokenManager{identity: "epoch"},
	}
	e := &sessionEntry{id: "sess", knowledge: knowledgeBinding{Account: "anonymous", Epoch: "epoch"}}
	app.sessions[e.id] = e

	app.tokens.mu.Lock()
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		close(started)
		errCh <- app.knowledgeAccountForRequest(context.Background(), e)
	}()
	<-started
	select {
	case err := <-errCh:
		app.tokens.mu.Unlock()
		t.Fatalf("returned while token lock held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	app.tokens.mu.Unlock()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("after unlock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not unblock after token unlock")
	}
}

func TestKnowledgeAccountForRequestRespectsCancel(t *testing.T) {
	app := &App{
		sessions: map[string]*sessionEntry{},
		tokens:   &tokenManager{identity: "epoch"},
	}
	e := &sessionEntry{id: "sess", knowledge: knowledgeBinding{Account: "anonymous", Epoch: "epoch"}}
	app.sessions[e.id] = e

	app.tokens.mu.Lock()
	defer app.tokens.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := app.knowledgeAccountForRequest(ctx, e)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline", err)
	}
}
