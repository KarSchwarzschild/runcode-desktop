package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"unicode/utf8"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"

	"gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// maxArtifactBytes caps how large a text artifact ReadArtifact returns, so a giant
// file cannot lock up the renderer. Larger files are opened externally instead.
const maxArtifactBytes = 2 << 20 // 2 MiB

// maxArtifactBinaryBytes caps ReadArtifactBytes. Office documents (docx/pptx/xlsx)
// are zip archives that the frontend renders in-place, so this is well above the
// text cap but still bounded — a huge file is opened externally instead of streamed
// as base64 (which inflates ~33%) through the Wails bridge and into the renderer.
const maxArtifactBinaryBytes = 25 << 20 // 25 MiB

// ReadArtifact returns UTF-8 text through a live source-session reference.
// It checks current read authority and caps output at maxArtifactBytes.
func (a *App) ReadArtifact(reference string) (string, error) {
	var text string
	err := a.withArtifact(context.Background(), reference, func(ctx context.Context, _ host.SessionAccess, e *artifactAccess) error {
		data, err := readArtifactFile(ctx, e.permit, maxArtifactBytes)
		if err != nil {
			return artifactFSError(e.ref.Path, err)
		}
		if !utf8.Valid(data) {
			return errors.New("file is not text")
		}
		text = string(data)
		return nil
	})
	if err != nil {
		return "", wireError(err)
	}
	return text, nil
}

// artifactFSError turns a filesystem failure on an artifact into something the
// user can act on.
//
// A preview is opened from a card in the conversation, so its path is a claim
// made when the reply was written and may have gone stale since — a later step
// (or the user) moves, renames or deletes the file, and the card still points at
// where it used to be. That is the common case here and deserves to say so.
//
// The raw error must not reach the UI: os.Stat on Windows reports the syscall it
// used, so a missing file surfaced as "GetFileAttributesEx D:\...\x.md: The system
// cannot find the file specified" — an absolute path plus the name of an API the
// user never called, to describe "that file isn't there any more". The path is
// echoed back in the workspace-relative form the card showed, not the resolved
// one, so it matches what the user is looking at.
func artifactFSError(relPath string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Coded, not just worded: a card pointing at a file that is gone is the one
		// case the UI wants to handle by *not* opening a tab at all, rather than by
		// showing text. Everything else stays a message, because there is something
		// for the user to react to.
		return &protocol.Error{
			Code:    protocol.ErrCodeNotFound,
			Message: fmt.Sprintf("文件不存在，可能已被移动、重命名或删除：%s", relPath),
		}
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("没有读取权限：%s", relPath)
	default:
		// Cause unknown (locked file, I/O error, unreadable mount): name the file in
		// the user's terms and keep the underlying error, which is all we have.
		return fmt.Errorf("读取文件失败：%s（%w）", relPath, err)
	}
}

// ReadArtifactBytes returns base64 bytes through the same source-session guard.
// The bridge avoids cross-origin fetch restrictions and works without an HTTP
// listener, allowing the frontend Office renderer to remain the safe fallback.
// It accepts a reference, never a bare path, and caps input at 25 MiB.
func (a *App) ReadArtifactBytes(reference string) (string, error) {
	var result string
	err := a.withArtifact(context.Background(), reference, func(ctx context.Context, _ host.SessionAccess, e *artifactAccess) error {
		data, err := readArtifactFile(ctx, e.permit, maxArtifactBinaryBytes)
		if err != nil {
			return artifactFSError(e.ref.Path, err)
		}
		result = base64.StdEncoding.EncodeToString(data)
		return nil
	})
	if err != nil {
		return "", wireError(err)
	}
	return result, nil
}

// previewRef 是一个工作区的预览服务器加它的引用数。
//
// 按工作区共享而不是按会话:同一目录开两个会话没有理由起两台服务器(两个端口、
// 两份文件句柄),而且两边看到的 URL 应该一致。refs 归零才真的停。
type previewRef struct {
	srv  *previewServer
	url  string
	refs int
}

// startPreview 为 workspace 取一台预览服务器:已经有就加一次引用,没有才起。
// 失败不致命(文本类预览走 ReadArtifact,不依赖这台服务器)。
func (a *App) startPreview(workspace string) {
	if workspace == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ref := a.previews[workspace]; ref != nil {
		ref.refs++
		return
	}
	ps := newPreviewServer()
	ps.handler = http.HandlerFunc(a.serveArtifact)
	url, err := ps.start(workspace)
	if err != nil {
		return
	}
	if a.previews == nil {
		a.previews = map[string]*previewRef{}
	}
	a.previews[workspace] = &previewRef{srv: ps, url: url, refs: 1}
}

// stopPreview 释放一次引用;归零时停掉服务器并从表里删掉。
// 传空工作区(会话本来就没开起来)是 no-op。
func (a *App) stopPreview(workspace string) {
	if workspace == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ref := a.previews[workspace]
	if ref == nil {
		return
	}
	ref.refs--
	if ref.refs > 0 {
		return
	}
	ref.srv.stop()
	delete(a.previews, workspace)
}

// previewBaseURL 是**聚焦会话**那个工作区的预览地址;没有会话或没起成服务器时
// 是空串(SessionInfo 里这一栏为空,前端据此退回 ReadArtifact 那条路)。
func (a *App) previewBaseURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.entryLocked(a.focused)
	if e == nil {
		return ""
	}
	if ref := a.previews[e.workspace]; ref != nil {
		return ref.url
	}
	return ""
}

// previewURLFor also works for a newly created, not-yet-focused branch.
func (a *App) previewURLFor(workspace string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ref := a.previews[workspace]; ref != nil {
		return ref.url
	}
	return ""
}
