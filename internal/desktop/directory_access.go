package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	desktopproto "github.com/wt68/runcode/internal/protocol"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/toolpath"
)

// DirectoryAccessRequest is the desktop directory-management request.
type DirectoryAccessRequest = desktopproto.DirectoryAccessRequest

// DirectoryAccessView reports explicit grants and their effective coverage.
type DirectoryAccessView = desktopproto.DirectoryAccessView

// ArtifactReference identifies one source-session preview.
type ArtifactReference = desktopproto.ArtifactReference

type permissionSession interface{ Permissions() *permissions.Service }

func permissionService(access host.SessionAccess) (*permissions.Service, error) {
	s, ok := access.Session.(permissionSession)
	if !ok || s.Permissions() == nil {
		return nil, permissions.ErrDirectoryAccessUnsupported
	}
	return s.Permissions(), nil
}
func (a *App) directoryAccess(req DirectoryAccessRequest, change string) (DirectoryAccessView, error) {
	var view DirectoryAccessView
	revokedKey := ""
	if req.SessionID == "" || a.mgr == nil {
		return view, wireError(errNoSession)
	}
	err := a.mgr.WithSessionOperation(context.Background(), req.SessionID, func(ctx context.Context, access host.SessionAccess) error {
		service, err := permissionService(access)
		if err != nil {
			return err
		}
		var snapshot permissions.DirectorySnapshot
		switch change {
		case "grant":
			dir := req.Directory
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(access.Workspace, dir)
			}
			canonical, pathErr := toolpath.Canonical(dir)
			if pathErr != nil {
				return pathErr
			}
			if canonical == filepath.Dir(canonical) && !req.ConfirmRoot {
				return fmt.Errorf("目录实际指向磁盘根目录：%s。请填写此实际路径并确认完整覆盖范围", canonical)
			}

			snapshot, err = service.GrantDirectoryAccess(ctx, canonical, req.Access, permissions.ApprovalScope(req.Scope))
		case "revoke":
			// The rule itself is revoked by its exact stored text; only the
			// in-flight preview match needs the canonical form grants were stored in.
			revokedKey = revokedDirectoryKey(access.Workspace, req.Directory)
			snapshot, err = service.RevokeDirectoryAccess(ctx, req.Directory, req.Access, permissions.ApprovalScope(req.Scope))
		case "":
			snapshot, err = service.ListDirectoryAccess(ctx)
		default:
			return errors.New("unknown directory operation")
		}
		if err != nil {
			return err
		}
		view = DirectoryAccessView{SessionID: req.SessionID, Workspace: access.Workspace, Revision: snapshot.Revision, Grants: []desktopproto.DirectoryAccessGrant{}}
		for _, g := range snapshot.Grants {
			view.Grants = append(view.Grants, desktopproto.DirectoryAccessGrant{Directory: g.Directory, Access: g.Access, Scope: string(g.Scope), CoveredBy: append([]string{}, g.CoveredBy...)})
		}
		return nil
	})
	if err == nil && change == "revoke" {
		a.cancelRevokedArtifactReads(req, view.Workspace, revokedKey)
	}
	if err == nil && change != "" {
		a.emitDirectoryAccessChanged(view.Workspace)
	}
	if err != nil {
		return DirectoryAccessView{}, wireError(err)
	}
	return view, nil
}

// revokedDirectoryKey resolves a revoked directory the way GrantDirectoryAccess
// stored it (workspace-relative → absolute → canonical), so relative input,
// Windows case or a symlinked parent such as macOS /var cannot dodge the
// preview cancellation. A directory that no longer exists falls back to its
// cleaned absolute form.
func revokedDirectoryKey(workspace, dir string) string {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(workspace, dir)
	}
	if canonical, err := toolpath.Canonical(dir); err == nil {
		return canonical
	}
	return filepath.Clean(dir)
}

// ListDirectoryAccess lists the named session and project directory rules.
func (a *App) ListDirectoryAccess(sessionID string) (DirectoryAccessView, error) {
	return a.directoryAccess(DirectoryAccessRequest{SessionID: sessionID}, "")
}

// GrantDirectoryAccess grants the explicitly selected directory and scope.
func (a *App) GrantDirectoryAccess(req DirectoryAccessRequest) (DirectoryAccessView, error) {
	return a.directoryAccess(req, "grant")
}

// RevokeDirectoryAccess removes only the selected scoped allow rule.
func (a *App) RevokeDirectoryAccess(req DirectoryAccessRequest) (DirectoryAccessView, error) {
	return a.directoryAccess(req, "revoke")
}

func (a *App) emitDirectoryAccessChanged(workspace string) {
	a.mu.Lock()
	var emitters []func(string, any)
	for _, e := range a.sessions {
		if e.workspace == workspace && !e.closed && e.emit != nil {
			emitters = append(emitters, e.emit)
		}
	}
	a.mu.Unlock()
	for _, emit := range emitters {
		emit(desktopproto.EventDirectoryAccessChanged, desktopproto.DirectoryAccessChanged{Workspace: workspace})
	}
}
