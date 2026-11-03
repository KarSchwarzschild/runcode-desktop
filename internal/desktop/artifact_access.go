package desktop

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/toolpath"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

const artifactLifetime = 4 * time.Hour
const maxArtifactReferences = 512

type artifactAccess struct {
	workspace string
	cancels   map[string]context.CancelFunc
	ref       ArtifactReference
	permit    *permissions.ReadPermit
	created   time.Time
	pdf       []byte
	version   string
}

func (a *App) issueArtifact(sessionID, generation, workspace string, permit *permissions.ReadPermit) (ArtifactReference, error) {
	if err := permit.Check(context.Background()); err != nil {
		return ArtifactReference{}, err
	}
	workspaceKey, err := toolpath.Canonical(workspace)
	if err != nil {
		return ArtifactReference{}, err
	}
	base := a.previewURLFor(workspace)
	a.artifactMu.Lock()
	defer a.artifactMu.Unlock()
	if a.artifacts == nil {
		a.artifacts = map[string]*artifactAccess{}
	}
	count := 0
	for id, old := range a.artifacts {
		if time.Since(old.created) > artifactLifetime {
			for _, cancel := range old.cancels {
				cancel()
			}
			delete(a.artifacts, id)
			continue
		}
		if old.ref.SessionID == sessionID && old.ref.Generation == generation {
			count++
		}
	}
	if len(a.artifacts) >= maxArtifactReferences || count >= 64 {
		return ArtifactReference{}, errors.New("预览引用过多，请关闭部分标签页后重试")
	}
	display := permit.Path()
	if rel, err := filepath.Rel(workspace, display); err == nil && filepath.IsLocal(rel) {
		display = rel
	}
	ref := ArtifactReference{Reference: rand.Text(), SessionID: sessionID, Generation: generation, Path: filepath.ToSlash(display)}
	volume := filepath.VolumeName(permit.Path()) + string(filepath.Separator)
	rel, err := filepath.Rel(volume, permit.Path())
	if err != nil {
		return ArtifactReference{}, err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	if base != "" {
		ref.URL = base + ref.Reference + "/file/" + strings.Join(parts, "/")
	}
	a.artifacts[ref.Reference] = &artifactAccess{ref: ref, permit: permit, created: time.Now(), workspace: workspaceKey, cancels: map[string]context.CancelFunc{}}
	return ref, nil
}

// OpenArtifact resolves a UI request in its named session, never the focus.
func (a *App) OpenArtifact(sessionID, path string) (ArtifactReference, error) {
	var ref ArtifactReference
	if sessionID == "" || a.mgr == nil {
		return ref, wireError(errNoSession)
	}
	err := a.mgr.WithSessionOperation(context.Background(), sessionID, func(ctx context.Context, access host.SessionAccess) error {
		service, err := permissionService(access)
		if err != nil {
			return err
		}
		permit, err := service.CheckReadAccess(ctx, access.Workspace, path)
		if err != nil {
			return err
		}
		f, err := permit.Open(ctx)
		if err != nil {
			return artifactFSError(path, err)
		}
		_ = f.Close()
		ref, err = a.issueArtifact(sessionID, access.Generation, access.Workspace, permit)
		return err
	})
	if err != nil {
		return ArtifactReference{}, wireError(err)
	}
	return ref, nil
}
func (a *App) previewOpener(sessionID, generation string) func(context.Context, string, *tool.Context) (any, error) {
	return func(ctx context.Context, path string, tctx *tool.Context) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		permit, err := permissions.ReadPermitFor(tctx, path)
		if err != nil {
			return nil, err
		}
		return a.issueArtifact(sessionID, generation, tctx.WorkingDirectory, permit)
	}
}
func (a *App) withArtifact(ctx context.Context, id string, fn func(context.Context, host.SessionAccess, *artifactAccess) error) error {
	a.artifactMu.Lock()
	if a.artifactIO == nil {
		a.artifactIO = make(chan struct{}, 4)
	}
	slots := a.artifactIO
	select {
	case slots <- struct{}{}:
	default:
		a.artifactMu.Unlock()
		return errors.New("预览读取繁忙，请稍后重试")
	}
	defer func() { <-slots }()
	original := a.artifacts[id]
	var entry artifactAccess
	if original != nil {
		entry = *original
	}
	a.artifactMu.Unlock()
	if original == nil || time.Since(entry.created) > artifactLifetime || a.mgr == nil {
		return errors.New("预览引用已失效，请重新打开文件")
	}
	return a.mgr.WithSessionOperation(ctx, entry.ref.SessionID, func(ctx context.Context, access host.SessionAccess) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		operation := rand.Text()
		a.artifactMu.Lock()
		if a.artifacts[id] != original {
			a.artifactMu.Unlock()
			return errors.New("预览引用已关闭")
		}
		original.cancels[operation] = cancel
		a.artifactMu.Unlock()
		defer func() { a.artifactMu.Lock(); delete(original.cancels, operation); a.artifactMu.Unlock() }()
		if access.Generation != entry.ref.Generation {
			return errors.New("来源会话已重新创建，请重新打开文件")
		}
		if err := entry.permit.Check(ctx); err != nil {
			return err
		}
		if err := fn(ctx, access, &entry); err != nil {
			return err
		}
		return entry.permit.Check(ctx)
	})
}

// ReleaseArtifact invalidates a closed preview capability without touching files.
func (a *App) ReleaseArtifact(reference string) {
	a.artifactMu.Lock()
	if entry := a.artifacts[reference]; entry != nil {
		for _, cancel := range entry.cancels {
			cancel()
		}
	}
	delete(a.artifacts, reference)
	a.artifactMu.Unlock()
}
func (a *App) dropArtifacts(sessionID string) {
	a.artifactMu.Lock()
	defer a.artifactMu.Unlock()
	for id, e := range a.artifacts {
		if e.ref.SessionID == sessionID {
			for _, cancel := range e.cancels {
				cancel()
			}
			delete(a.artifacts, id)
		}
	}
}
func readArtifactFile(ctx context.Context, permit *permissions.ReadPermit, limit int64) ([]byte, error) {
	f, err := permit.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, errors.New("file too large to preview")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file too large to preview")
	}
	if err := permit.Check(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

// Cancel only affected previews; unrelated model turns and directories continue.
func (a *App) cancelRevokedArtifactReads(req DirectoryAccessRequest, workspace, revokedKey string) {
	workspaceKey, err := toolpath.Canonical(workspace)
	if err != nil {
		return
	}
	a.artifactMu.Lock()
	defer a.artifactMu.Unlock()
	for _, e := range a.artifacts {
		if req.Scope == "project" {
			same, _ := toolpath.IsWithin(workspaceKey, e.workspace)
			reverse, _ := toolpath.IsWithin(e.workspace, workspaceKey)
			if !same || !reverse {
				continue
			}
		} else if e.ref.SessionID != req.SessionID {
			continue
		}
		within, err := toolpath.IsWithin(revokedKey, e.permit.Path())
		if err != nil || !within {
			continue
		}
		for _, cancel := range e.cancels {
			cancel()
		}
	}
}
