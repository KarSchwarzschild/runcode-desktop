package desktop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wt68/runcode/internal/automation"
)

const exitSummaryTimeout = 5 * time.Second

// ExitConfirmation is the native prompt data prepared before any cleanup starts.
// SummaryKnown is false when the task database could not be queried.
type ExitConfirmation struct {
	EnabledCount    int
	UnfinishedCount int
	SummaryKnown    bool
	Message         string
}

// WindowLifecycleOptions are the platform-neutral lifecycle seams. The Wails
// shell supplies window operations, native confirmation and process quit; the
// scheduler/exit callbacks stay testable without a GUI.
type WindowLifecycleOptions struct {
	BackgroundSupported bool
	Hide                func() error
	UnMinimise          func()
	Show                func()
	Focus               func()
	ConfirmExit         func(ExitConfirmation, func(bool))
	ExitSummary         func(context.Context) (automation.ExitSummary, error)
	HoldAdmission       func(reason string) (release func(), err error)
	PrepareExit         func(time.Duration) bool
	Quit                func()
	ReportError         func(error)
}

// WindowHostOptions are the host-only half used by NewAppWindowLifecycle.
type WindowHostOptions struct {
	BackgroundSupported bool
	Hide                func() error
	UnMinimise          func()
	Show                func()
	Focus               func()
	ConfirmExit         func(ExitConfirmation, func(bool))
	Quit                func()
	ReportError         func(error)
}

// WindowLifecycle implements the native close/exit policy. Closing a supported
// background host only hides the window. A real exit asks before holding
// scheduler admission, then runs the shared bounded cleanup and quits.
type WindowLifecycle struct {
	mu         sync.Mutex
	opts       WindowLifecycleOptions
	confirming bool
	quitting   bool
	hold       func()
}

func NewWindowLifecycle(opts WindowLifecycleOptions) *WindowLifecycle {
	return &WindowLifecycle{opts: opts}
}

// NewAppWindowLifecycle wires the native host to one desktop App. This factory
// can access private scheduler/exit methods without exposing them as frontend
// commands.
func NewAppWindowLifecycle(app *App, host WindowHostOptions) *WindowLifecycle {
	return NewWindowLifecycle(WindowLifecycleOptions{
		BackgroundSupported: host.BackgroundSupported,
		Hide:                host.Hide,
		UnMinimise:          host.UnMinimise,
		Show:                host.Show,
		Focus:               host.Focus,
		ConfirmExit:         host.ConfirmExit,
		ExitSummary:         app.automationExitSummary,
		HoldAdmission:       app.holdAutomationAdmission,
		PrepareExit:         func(limit time.Duration) bool { return app.PrepareExit(limit) },
		Quit:                host.Quit,
		ReportError:         host.ReportError,
	})
}

func (l *WindowLifecycle) BackgroundSupported() bool {
	return l != nil && l.opts.BackgroundSupported
}

func (l *WindowLifecycle) Quitting() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.quitting
}

func (l *WindowLifecycle) CloseMainWindow() error {
	if l == nil {
		return errors.New("窗口生命周期不可用")
	}
	if !l.opts.BackgroundSupported {
		return l.RequestAppExit()
	}
	if l.Quitting() {
		return nil
	}
	if l.opts.Hide == nil {
		return errors.New("无法隐藏主窗口")
	}
	if err := l.opts.Hide(); err != nil {
		_ = l.RestoreMainWindow()
		return fmt.Errorf("隐藏主窗口失败: %w", err)
	}
	return nil
}

func (l *WindowLifecycle) RestoreMainWindow() error {
	if l == nil {
		return errors.New("窗口生命周期不可用")
	}
	if l.opts.UnMinimise == nil || l.opts.Show == nil || l.opts.Focus == nil {
		return errors.New("窗口恢复接口不可用")
	}
	l.opts.UnMinimise()
	l.opts.Show()
	l.opts.Focus()
	return nil
}

// PendingCount is used by the tray menu. Only unfinished runs make the
// "查看待处理任务" entry visible.
func (l *WindowLifecycle) PendingCount(ctx context.Context) (int, error) {
	if l == nil || l.opts.ExitSummary == nil {
		return 0, errors.New("无法查询后台任务状态")
	}
	summary, err := l.opts.ExitSummary(ctx)
	if err != nil {
		return 0, err
	}
	return summary.UnfinishedCount, nil
}

func (l *WindowLifecycle) RequestAppExit() error {
	if l == nil {
		return errors.New("窗口生命周期不可用")
	}
	l.mu.Lock()
	if l.quitting || l.confirming {
		l.mu.Unlock()
		return nil
	}
	l.confirming = true
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), exitSummaryTimeout)
	summary, summaryErr := automation.ExitSummary{}, error(nil)
	if l.opts.ExitSummary != nil {
		summary, summaryErr = l.opts.ExitSummary(ctx)
	}
	cancel()
	needsConfirmation := summaryErr != nil || summary.EnabledCount > 0 || summary.UnfinishedCount > 0
	if !needsConfirmation {
		l.finishExit(true)
		return nil
	}
	if l.opts.ConfirmExit == nil {
		l.cancelConfirmation()
		return errors.New("原生退出确认不可用")
	}
	l.opts.ConfirmExit(exitConfirmation(summary, summaryErr), l.finishExit)
	return nil
}

func exitConfirmation(summary automation.ExitSummary, summaryErr error) ExitConfirmation {
	known := summaryErr == nil
	var message string
	if known {
		message = fmt.Sprintf(
			"退出后自动化任务将无法执行。\n\n已开启任务：%d 个\n未完成运行：%d 个\n\n确定要退出吗？",
			summary.EnabledCount, summary.UnfinishedCount,
		)
	} else {
		message = "无法确认后台任务状态。\n\n退出后自动化任务将无法执行，确定仍要退出吗？"
	}
	return ExitConfirmation{
		EnabledCount: summary.EnabledCount, UnfinishedCount: summary.UnfinishedCount,
		SummaryKnown: known, Message: message,
	}
}

func (l *WindowLifecycle) finishExit(confirmed bool) {
	l.mu.Lock()
	if !l.confirming {
		l.mu.Unlock()
		return
	}
	if !confirmed {
		l.confirming = false
		l.mu.Unlock()
		return
	}
	if l.quitting {
		l.confirming = false
		l.mu.Unlock()
		return
	}
	if l.opts.HoldAdmission == nil {
		l.confirming = false
		l.mu.Unlock()
		l.report(errors.New("无法停止新任务派发"))
		return
	}
	release, err := l.opts.HoldAdmission("exit")
	if err != nil {
		l.confirming = false
		l.mu.Unlock()
		l.report(fmt.Errorf("无法停止新任务派发: %w", err))
		return
	}
	if l.opts.PrepareExit == nil || l.opts.Quit == nil {
		if release != nil {
			release()
		}
		l.confirming = false
		l.mu.Unlock()
		l.report(errors.New("退出收尾接口不可用"))
		return
	}
	l.hold = release
	l.confirming = false
	l.quitting = true
	l.mu.Unlock()

	go func() {
		l.opts.PrepareExit(ExitTimeout)
		l.opts.Quit()
	}()
}

func (l *WindowLifecycle) cancelConfirmation() {
	l.mu.Lock()
	l.confirming = false
	l.mu.Unlock()
}

func (l *WindowLifecycle) report(err error) {
	if err != nil && l.opts.ReportError != nil {
		l.opts.ReportError(err)
	}
}

func (a *App) automationExitSummary(ctx context.Context) (automation.ExitSummary, error) {
	svc := a.automationService()
	if svc == nil {
		return automation.ExitSummary{}, errAutomationUnavailable
	}
	return svc.ExitSummary(ctx)
}

func (a *App) holdAutomationAdmission(reason string) (func(), error) {
	svc := a.automationService()
	if svc == nil {
		return nil, errAutomationUnavailable
	}
	return svc.HoldAdmission(reason), nil
}

func (a *App) automationBackgroundSupported() bool {
	a.autoCtl.mu.Lock()
	w := a.autoCtl.window
	a.autoCtl.mu.Unlock()
	return w != nil && w.BackgroundSupported()
}
