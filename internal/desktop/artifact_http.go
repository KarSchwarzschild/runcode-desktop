package desktop

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/toolpath"
)

// serveArtifact exposes only session-bound references, never a bare workspace.
func (a *App) serveArtifact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	served := false
	err := a.withArtifact(r.Context(), parts[0], func(ctx context.Context, access host.SessionAccess, e *artifactAccess) error {
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		stop := context.AfterFunc(ctx, func() { _ = controller.SetWriteDeadline(time.Now()) })
		defer stop()
		permit := e.permit
		if parts[1] == "pdf" {
			if len(e.pdf) == 0 {
				return errors.New("PDF is not ready")
			}
			info, err := os.Stat(permit.Path())
			if err != nil || officeCacheName(permit.Path(), info) != e.version {
				return errors.New("source changed")
			}
			if err := permit.Check(ctx); err != nil {
				return err
			}
			served = true
			http.ServeContent(w, r, "preview.pdf", info.ModTime(), bytes.NewReader(e.pdf))
			return nil
		}
		if parts[1] != "file" || len(parts) != 3 {
			return errors.New("invalid preview route")
		}
		rel := filepath.FromSlash(parts[2])
		if !filepath.IsLocal(rel) {
			return errors.New("invalid resource path")
		}
		target := filepath.Join(filepath.VolumeName(permit.Path())+string(filepath.Separator), rel)
		if strings.Contains(strings.ToLower(filepath.ToSlash(target)), "/.runcode/"+previewCacheDir+"/") {
			return errors.New("legacy preview cache is private")
		}
		target, err := toolpath.Canonical(target)
		if err != nil {
			return err
		}
		same := target == permit.Path()
		if runtime.GOOS == "windows" {
			same = strings.EqualFold(target, permit.Path())
		}
		if !same {
			service, err := permissionService(access)
			if err != nil {
				return err
			}
			permit, err = service.CheckReadAccess(ctx, access.Workspace, target)
			if err != nil {
				return err
			}
		}
		f, err := permit.Open(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() > maxArtifactBinaryBytes {
			return errors.New("file too large to preview")
		}
		served = true
		http.ServeContent(w, r, filepath.Base(target), info.ModTime(), f)
		return nil
	})
	if err != nil && !served {
		http.Error(w, "预览文件不可用或读取授权已失效", http.StatusForbidden)
	}
}
