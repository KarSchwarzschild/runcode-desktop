package desktop

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
)

// maxListedFiles caps the workspace file list returned for the @-mention picker,
// so a huge repo cannot bloat the payload or the UI. The frontend filters this
// list as the user types.
const maxListedFiles = 4000

// skippedListDirs are directories never surfaced in the file picker: VCS/editor
// metadata, runcode's own bookkeeping, and heavy generated trees.
var skippedListDirs = map[string]bool{
	".git": true, ".runcode": true, "node_modules": true,
	".idea": true, ".vscode": true, ".venv": true, "__pycache__": true,
}

// ListFiles returns the active session workspace's files as sorted, slash-style
// relative paths, for the composer's @-mention file picker. Nil when there is no
// workspace. The list is capped; the frontend filters it by the typed query.
func (a *App) ListFiles() []string {
	a.mu.Lock()
	ws := a.workspace
	a.mu.Unlock()
	return listWorkspaceFiles(ws)
}

// ListSessionFiles never borrows the focused workspace for an async request.
func (a *App) ListSessionFiles(sessionID string) ([]string, error) {
	var files []string
	if sessionID == "" || a.mgr == nil {
		return nil, wireError(errNoSession)
	}
	err := a.mgr.WithSessionOperation(context.Background(), sessionID, func(_ context.Context, access host.SessionAccess) error {
		files = listWorkspaceFiles(access.Workspace)
		return nil
	})
	return files, wireError(err)
}

func listWorkspaceFiles(ws string) []string {
	if ws == "" {
		return nil
	}
	out := make([]string, 0, 256)
	_ = filepath.WalkDir(ws, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != ws && skippedListDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(ws, p)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= maxListedFiles {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}
