package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wt68/runcode/internal/automation"
)

type lifecycleHarness struct {
	mu         sync.Mutex
	events     []string
	prompts    []ExitConfirmation
	hideErr    error
	summary    automation.ExitSummary
	summaryErr error
	holdErr    error
	prepare    chan bool
	quit       chan struct{}
	reported   chan error
}

func newLifecycleHarness(t *testing.T) (*WindowLifecycle, *lifecycleHarness) {
	t.Helper()
	h := &lifecycleHarness{
		prepare:  make(chan bool, 1),
		quit:     make(chan struct{}, 1),
		reported: make(chan error, 1),
	}
	life := NewWindowLifecycle(WindowLifecycleOptions{
		BackgroundSupported: true,
		Hide: func() error {
			h.record("hide")
			return h.hideErr
		},
		UnMinimise: func() { h.record("unminimise") },
		Show:       func() { h.record("show") },
		Focus:      func() { h.record("focus") },
		ConfirmExit: func(prompt ExitConfirmation, decide func(bool)) {
			h.mu.Lock()
			h.prompts = append(h.prompts, prompt)
			h.mu.Unlock()
			decide(false)
		},
		ExitSummary: func(context.Context) (automation.ExitSummary, error) {
			return h.summary, h.summaryErr
		},
		HoldAdmission: func(string) (func(), error) {
			h.record("hold")
			if h.holdErr != nil {
				return nil, h.holdErr
			}
			return func() { h.record("release") }, nil
		},
		PrepareExit: func(time.Duration) bool {
			h.record("prepare")
			h.prepare <- true
			return true
		},
		Quit: func() {
			h.record("quit")
			h.quit <- struct{}{}
		},
		ReportError: func(err error) { h.reported <- err },
	})
	return life, h
}

func (h *lifecycleHarness) record(event string) {
	h.mu.Lock()
	h.events = append(h.events, event)
	h.mu.Unlock()
}

func (h *lifecycleHarness) eventList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

func (h *lifecycleHarness) promptList() []ExitConfirmation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ExitConfirmation(nil), h.prompts...)
}

func TestWindowCloseHidesWithoutCleanup(t *testing.T) {
	life, h := newLifecycleHarness(t)
	if err := life.CloseMainWindow(); err != nil {
		t.Fatal(err)
	}
	if got := h.eventList(); len(got) != 1 || got[0] != "hide" {
		t.Fatalf("events = %v", got)
	}
	if life.Quitting() {
		t.Fatal("closing the main window entered the quitting state")
	}
	select {
	case <-h.prepare:
		t.Fatal("window close started PrepareExit")
	case <-h.quit:
		t.Fatal("window close quit the app")
	default:
	}
}

func TestWindowRestoreKeepsState(t *testing.T) {
	life, h := newLifecycleHarness(t)
	if err := life.RestoreMainWindow(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.eventList(), ","); got != "unminimise,show,focus" {
		t.Fatalf("restore order = %s", got)
	}
}

func TestExitConfirmationCounts(t *testing.T) {
	cases := []struct {
		name    string
		summary automation.ExitSummary
		err     error
		prompt  bool
	}{
		{name: "enabled", summary: automation.ExitSummary{EnabledCount: 1}, prompt: true},
		{name: "active paused task", summary: automation.ExitSummary{UnfinishedCount: 1}, prompt: true},
		{name: "allstopped", summary: automation.ExitSummary{}, prompt: false},
		{name: "unknown", err: errors.New("database unavailable"), prompt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			life, h := newLifecycleHarness(t)
			h.summary = tc.summary
			h.summaryErr = tc.err
			if err := life.RequestAppExit(); err != nil {
				t.Fatal(err)
			}
			prompts := h.promptList()
			if tc.prompt {
				if len(prompts) != 1 {
					t.Fatalf("prompts = %d, want 1", len(prompts))
				}
				if !strings.Contains(prompts[0].Message, "退出后自动化任务") {
					t.Fatalf("prompt = %q", prompts[0].Message)
				}
				if tc.err != nil && !strings.Contains(prompts[0].Message, "无法确认后台任务状态") {
					t.Fatalf("unknown-summary prompt = %q", prompts[0].Message)
				}
				return
			}
			if len(prompts) != 0 {
				t.Fatalf("unexpected prompt: %+v", prompts)
			}
			select {
			case <-h.prepare:
			case <-time.After(time.Second):
				t.Fatal("no-prompt exit did not prepare cleanup")
			}
			select {
			case <-h.quit:
			case <-time.After(time.Second):
				t.Fatal("no-prompt exit did not quit")
			}
		})
	}
}

func TestExitCancelKeepsScheduler(t *testing.T) {
	life, h := newLifecycleHarness(t)
	h.summary.EnabledCount = 1
	if err := life.RequestAppExit(); err != nil {
		t.Fatal(err)
	}
	if len(h.promptList()) != 1 {
		t.Fatal("expected confirmation")
	}
	if life.Quitting() {
		t.Fatal("cancel set quitting")
	}
	for _, event := range h.eventList() {
		if event == "hold" || event == "prepare" || event == "quit" {
			t.Fatalf("cancel changed lifecycle: %v", h.eventList())
		}
	}
}

func TestExitDoubleClickOneDialog(t *testing.T) {
	life, h := newLifecycleHarness(t)
	h.summary.EnabledCount = 1
	decide := make(chan func(bool), 1)
	life.opts.ConfirmExit = func(prompt ExitConfirmation, cb func(bool)) {
		h.mu.Lock()
		h.prompts = append(h.prompts, prompt)
		h.mu.Unlock()
		decide <- cb
	}
	if err := life.RequestAppExit(); err != nil {
		t.Fatal(err)
	}
	if err := life.RequestAppExit(); err != nil {
		t.Fatal(err)
	}
	if got := len(h.promptList()); got != 1 {
		t.Fatalf("dialogs = %d, want 1", got)
	}
	(<-decide)(false)
}

func TestExitAdmissionRace(t *testing.T) {
	life, h := newLifecycleHarness(t)
	h.summary.EnabledCount = 1
	life.opts.ConfirmExit = func(_ ExitConfirmation, decide func(bool)) { decide(true) }
	if err := life.RequestAppExit(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.prepare:
	case <-time.After(time.Second):
		t.Fatal("confirmed exit did not prepare cleanup")
	}
	select {
	case <-h.quit:
	case <-time.After(time.Second):
		t.Fatal("confirmed exit did not quit")
	}
	if got := strings.Join(h.eventList(), ","); got != "hold,prepare,quit" {
		t.Fatalf("exit order = %s", got)
	}
	if !life.Quitting() {
		t.Fatal("confirmed exit was not marked quitting")
	}
}

func TestExitHoldFailureKeepsProcessAlive(t *testing.T) {
	life, h := newLifecycleHarness(t)
	h.summary.EnabledCount = 1
	h.holdErr = errors.New("service unavailable")
	life.opts.ConfirmExit = func(_ ExitConfirmation, decide func(bool)) { decide(true) }
	if err := life.RequestAppExit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-h.reported:
		if !strings.Contains(err.Error(), "停止新任务派发") {
			t.Fatalf("reported = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hold failure was not reported")
	}
	if life.Quitting() {
		t.Fatal("hold failure still marked quitting")
	}
	select {
	case <-h.quit:
		t.Fatal("hold failure quit the process")
	default:
	}
}
