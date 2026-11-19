//go:build !kylin

package main

import (
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
		Quit:        app.Quit,
		ReportError: report,
	})
	installAutomationTray(app, mainWin, life, title, restore)
	return life
}
