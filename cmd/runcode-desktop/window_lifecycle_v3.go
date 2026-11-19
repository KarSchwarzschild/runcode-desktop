//go:build !kylin

package main

import (
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wt68/runcode/internal/desktop"
)

func createDesktopWindowLifecycle(
	app *application.App,
	mainWin *application.WebviewWindow,
	deskApp *desktop.App,
	title string,
) *desktop.WindowLifecycle {
	var life *desktop.WindowLifecycle
	restore := func() {
		if life != nil {
			_ = life.RestoreMainWindow()
		}
	}
	report := func(err error) {
		if err == nil {
			return
		}
		app.Dialog.Error().
			SetTitle("窗口操作失败").
			SetMessage(err.Error()).
			Show()
	}
	life = desktop.NewAppWindowLifecycle(deskApp, desktop.WindowHostOptions{
		BackgroundSupported: runtime.GOOS == "windows",
		Hide: func() error {
			mainWin.Hide()
			return nil
		},
		UnMinimise: mainWin.UnMinimise,
		Show:       func() { mainWin.Show() },
		Focus:      mainWin.Focus,
		ConfirmExit: func(prompt desktop.ExitConfirmation, decide func(bool)) {
			dialog := app.Dialog.Question().
				SetTitle("退出" + title).
				SetMessage(prompt.Message)
			cancel := dialog.AddButton("取消").OnClick(func() { decide(false) })
			dialog.AddButton("退出").OnClick(func() { decide(true) })
			dialog.SetDefaultButton(cancel)
			dialog.SetCancelButton(cancel)
			dialog.Show()
		},
		ConfirmClose: func(prompt desktop.CloseConfirmation, decide func(desktop.CloseChoice)) {
			dialog := app.Dialog.Question().
				SetTitle("关闭" + title).
				SetMessage(prompt.Message)
			hide := dialog.AddButton("隐藏主窗").OnClick(func() { decide(desktop.CloseHide) })
			dialog.AddButton("直接退出").OnClick(func() { decide(desktop.CloseExit) })
			cancel := dialog.AddButton("取消").OnClick(func() { decide(desktop.CloseCancel) })
			dialog.SetDefaultButton(hide)
			// 按 Esc 或点确认框的关闭按钮都取消本次操作，窗口和调度保持原状。
			dialog.SetCancelButton(cancel)
			dialog.Show()
		},
		Quit: app.Quit,
		ForceQuit: func() {
			if runtime.GOOS == "windows" {
				os.Exit(0)
			}
		},
		ReportError: report,
	})
	installAutomationTray(app, mainWin, life, title, restore)
	return life
}
