package desktop

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
)

// OpenExternal hands the authorized source file to the OS default application.
func (a *App) OpenExternal(reference string) error {
	return wireError(a.withArtifactPath(reference, func(path string) error { return startAndReap(openCommand(path)) }))
}

// RevealInFolder shows the authorized source file in the OS file manager.
func (a *App) RevealInFolder(reference string) error {
	return wireError(a.withArtifactPath(reference, func(path string) error { return startAndReap(revealCommand(path)) }))
}

func (a *App) withArtifactPath(reference string, fn func(string) error) error {
	return a.withArtifact(context.Background(), reference, func(ctx context.Context, _ host.SessionAccess, e *artifactAccess) error {
		f, err := e.permit.Open(ctx)
		if err != nil {
			return artifactFSError(e.ref.Path, err)
		}
		_ = f.Close()
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(e.permit.Path())
	})
}

// startAndReap starts cmd and reaps it in the background once it exits. The
// launchers here (explorer/open/xdg-open, browser handoff) are fire-and-forget,
// but a child that is never Wait()ed lingers as a zombie on POSIX until the app
// itself exits — and open/reveal are high-frequency actions over a long session.
func startAndReap(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ResolveArtifactPath returns the authorized source path for the UI copy action.
func (a *App) ResolveArtifactPath(reference string) (string, error) {
	full, err := a.resolveArtifact(reference)
	return full, wireError(err)
}
func (a *App) resolveArtifact(reference string) (string, error) {
	var path string
	err := a.withArtifactPath(reference, func(source string) error { path = source; return nil })
	if err != nil {
		return "", err
	}
	return path, nil
}

// openCommand builds the command to open path with the OS default app WITHOUT a
// shell, so an attacker-chosen filename (the workspace is AI-written) cannot inject
// commands — each binary is invoked directly with the path as an inert argv element.
// Windows uses explorer.exe (opens a file with its default handler); rundll32 was
// tried but is unreliable for local files. If explorer ever proves flaky for some
// type, switch to the ShellExecute API (build-tagged) — do NOT reintroduce cmd /c start.
func openCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", path)
	case "darwin":
		return exec.Command("open", path)
	default:
		return exec.Command("xdg-open", path)
	}
}

// revealCommand builds the command to reveal path in the OS file manager, also
// shell-free (explorer/open/xdg-open invoked directly).
func revealCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	// G204 在这里是误报,原因见上:二进制名是常量、不经 shell、path 已过
	// source-bound ReadPermit,拼进 argv 的都是惰性元素而非可解释的命令串。
	case "windows":
		return exec.Command("explorer", "/select,"+path) //nolint:gosec // G204
	case "darwin":
		return exec.Command("open", "-R", path) //nolint:gosec // G204
	default:
		return exec.Command("xdg-open", filepath.Dir(path)) //nolint:gosec // G204
	}
}
