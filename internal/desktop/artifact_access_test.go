package desktop

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/executil"
	"gitlab.ouc-online.com.cn/aibase/agentloop/toolpath"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

type permissionHostSession struct {
	*fakeHostSession
	perms *permissions.Service
}

func (s *permissionHostSession) Permissions() *permissions.Service { return s.perms }

func previewFixture(t *testing.T, ws string) (*App, string) {
	t.Helper()
	a := newWithBuild(&recordingSink{}, func(cfg engine.Config, opts engine.Options) (host.Session, error) {
		fake := newFakeHostSession()
		fake.id, fake.cwd = cfg.SessionID, cfg.CWD
		return &permissionHostSession{fake, opts.Permissions}, nil
	})
	id, _, err := a.mgr.Create(context.Background(), engine.Config{CWD: ws, Model: "fixture", PermissionMode: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	focusOn(a, id, ws)
	a.workspace = ws
	a.startPreview(ws)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.mgr.CloseAll(ctx)
		a.stopPreview(ws)
	})
	return a, id
}
func artifactID(t *testing.T, a *App, path string) string {
	t.Helper()
	a.mu.Lock()
	id := a.focused
	a.mu.Unlock()
	ref, err := a.OpenArtifact(id, path)
	if err != nil {
		t.Fatal(err)
	}
	return ref.Reference
}
func checkPreviewHTTP(t *testing.T, method, url string, status int) string {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if method == "RANGE" {
		req.Method = http.MethodGet
		req.Header.Set("Range", "bytes=0-3")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("%s %s: status=%d body=%s", method, url, resp.StatusCode, body)
	}
	return string(body)
}
func TestArtifactReferencesLiveRevokeAndSourceSession(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "资料.txt")
	if err := os.WriteFile(file, []byte("first fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a, id := previewFixture(t, ws)
	if _, err := a.OpenArtifact(id, file); err == nil {
		t.Fatal("unapproved external preview")
	}
	if _, err := a.GrantDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	ref, err := a.OpenArtifact(id, file)
	if err != nil {
		t.Fatal(err)
	}
	focusOn(a, "other-focus", t.TempDir())
	a.workspace = t.TempDir()
	got, err := a.ReadArtifact(ref.Reference)
	if err != nil || got != "first fixture" {
		t.Fatalf("source changed with focus: %q %v", got, err)
	}
	checkPreviewHTTP(t, "GET", ref.URL, 200)
	checkPreviewHTTP(t, "HEAD", ref.URL, 200)
	if got := checkPreviewHTTP(t, "RANGE", ref.URL, 206); got != "firs" {
		t.Fatal(got)
	}
	if _, err := a.RevokeDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadArtifact(ref.Reference); err == nil {
		t.Fatal("reference retained revoked directory access")
	}
	checkPreviewHTTP(t, "GET", ref.URL, 403)
	checkPreviewHTTP(t, "HEAD", ref.URL, 403)
}
func TestPreviewOnceRejectsSiblingsAndBarePaths(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "index.html")
	if err := os.WriteFile(file, []byte("<html>fixture</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "sibling.txt"), []byte("unapproved"), 0600); err != nil {
		t.Fatal(err)
	}
	a, id := previewFixture(t, ws)
	var permit *permissions.ReadPermit
	var generation string
	err := a.mgr.WithSessionOperation(context.Background(), id, func(ctx context.Context, access host.SessionAccess) error {
		service, _ := permissionService(access)
		action, err := permissions.ResolveReadPaths("open_preview", []string{file}, nil)
		if err != nil {
			return err
		}
		decision := permissions.Allow(permissions.ReasonApprovalGranted, "fixture.once")
		decision.ApprovalScope = permissions.ApprovalScopeOnce
		req := &tool.Context{WorkingDirectory: ws}
		req.ReadAccess = service.CallReadAccess(ctx, action, decision, req)
		permit, err = permissions.ReadPermitFor(req, file)
		generation = access.Generation
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.issueArtifact(id, generation, ws, permit)
	if err != nil {
		t.Fatal(err)
	}
	checkPreviewHTTP(t, "GET", ref.URL, 200)
	resource, err := url.Parse(ref.URL)
	if err != nil {
		t.Fatal(err)
	}
	resource.Path = filepath.ToSlash(filepath.Join(filepath.Dir(resource.Path), "sibling.txt"))
	checkPreviewHTTP(t, "GET", resource.String(), 403)
	checkPreviewHTTP(t, "GET", a.previewURLFor(ws)+"unprotected.txt", 404)
	a.ReleaseArtifact(ref.Reference)
	checkPreviewHTTP(t, "GET", ref.URL, 403)
}

func TestOfficePDFCacheCannotOutliveSourceGrant(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(outside, "report.docx")
	if err := os.WriteFile(source, []byte("synthetic office source"), 0600); err != nil {
		t.Fatal(err)
	}
	a, id := previewFixture(t, ws)
	if _, err := a.GrantDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "session"}); err != nil {
		t.Fatal(err)
	}
	ref, err := a.OpenArtifact(id, source)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	a.artifactMu.Lock()
	a.artifacts[ref.Reference].pdf = []byte("%PDF-1.7 synthetic cache")
	a.artifacts[ref.Reference].version = officeCacheName(source, info)
	a.artifactMu.Unlock()
	pdfURL := strings.Split(ref.URL, "/file/")[0] + "/pdf"
	checkPreviewHTTP(t, "GET", pdfURL, 200)
	if _, err := a.RevokeDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "session"}); err != nil {
		t.Fatal(err)
	}
	checkPreviewHTTP(t, "GET", pdfURL, 403)
	checkPreviewHTTP(t, "RANGE", pdfURL, 403)
}

func TestArtifactClosingSessionRejectsNewIOAndPreservesReference(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "source.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a, id := previewFixture(t, ws)
	ref, err := a.OpenArtifact(id, file)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.mgr.WithSessionOperation(context.Background(), id, func(ctx context.Context, _ host.SessionAccess) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-finish
			return ctx.Err()
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = a.mgr.Close(ctx, id)
	cancel()
	if err == nil {
		t.Fatal("close released in-flight operation")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("operation not canceled")
	}
	if _, err := a.ReadArtifact(ref.Reference); err == nil {
		t.Fatal("closing accepted new reads")
	}
	a.artifactMu.Lock()
	stillPresent := a.artifacts[ref.Reference] != nil
	a.artifactMu.Unlock()
	if !stillPresent {
		t.Fatal("failed close discarded reference")
	}
	close(finish)
	<-done
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.mgr.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadArtifact(ref.Reference); err == nil {
		t.Fatal("closed reference still usable")
	}
}

func TestArtifactRevocationCancelsOnlyAffectedReads(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	a, id := previewFixture(t, ws)
	req := DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "project"}
	if _, err := a.GrantDirectoryAccess(req); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(outside, "slow.txt")
	local := filepath.Join(ws, "local.txt")
	for _, p := range []string{file, local} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := a.OpenArtifact(id, file)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.OpenArtifact(id, local)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		done <- a.withArtifact(ctx, ref.Reference, func(ctx context.Context, _ host.SessionAccess, _ *artifactAccess) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-entered
	if _, err := a.RevokeDirectoryAccess(req); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("revoked read not canceled: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel active read")
	}
	if got, err := a.ReadArtifact(ref.Reference); err == nil || got != "" {
		t.Fatalf("revoked read returned data: %q %v", got, err)
	}
	if got, err := a.ReadArtifact(other.Reference); err != nil || got != "fixture" {
		t.Fatalf("unrelated read canceled: %q %v", got, err)
	}
}

func TestArtifactReferenceDoesNotSurviveSameIDRecreation(t *testing.T) {
	ws := t.TempDir()
	a, id := previewFixture(t, ws)
	file := filepath.Join(ws, "same.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := a.OpenArtifact(id, file)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately bypass desktop cleanup: the host generation still protects the
	// old reference when the same logical ID is reused.
	if err := a.mgr.Close(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.mgr.Create(context.Background(), engine.Config{SessionID: id, CWD: ws, Model: "fixture", PermissionMode: "safe"}); err != nil {
		t.Fatal(err)
	}
	if got, err := a.ReadArtifact(ref.Reference); err == nil || got != "" {
		t.Fatalf("old generation read: %q %v", got, err)
	}
	next, err := a.OpenArtifact(id, file)
	if err != nil {
		t.Fatal(err)
	}
	if next.Generation == ref.Generation {
		t.Fatal("recreated host reused generation")
	}
	checkPreviewHTTP(t, "GET", next.URL, 200)
	checkPreviewHTTP(t, "GET", ref.URL, 403)
}

func TestAuthorizedArtifactResourcesRejectLinkRetarget(t *testing.T) {
	for _, kind := range []string{"symlink", "junction"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "junction" && runtime.GOOS != "windows" {
				t.Skip("Windows junction")
			}
			ws, outside := t.TempDir(), t.TempDir()
			a, id := previewFixture(t, ws)
			name := "资料 #%.txt"
			source := filepath.Join(ws, name)
			if err := os.WriteFile(source, []byte("authorized"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, name), []byte("private"), 0600); err != nil {
				t.Fatal(err)
			}
			ref, err := a.OpenArtifact(id, source)
			if err != nil {
				t.Fatal(err)
			}
			if got := checkPreviewHTTP(t, "GET", ref.URL, 200); got != "authorized" {
				t.Fatal(got)
			}
			link := filepath.Join(ws, "redirect")
			if kind == "junction" {
				cmd := exec.Command("cmd", "/c", "mklink", "/J", link, outside)
				executil.HideConsoleWindow(cmd)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Skipf("junction unavailable: %v %s", err, out)
				}
			} else if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			resource, err := url.Parse(ref.URL)
			if err != nil {
				t.Fatal(err)
			}
			resource.Path = path.Join(path.Dir(resource.Path), "redirect", name)
			resource.RawPath = ""
			checkPreviewHTTP(t, "GET", resource.String(), 403)
			if _, err := a.OpenArtifact(id, filepath.Join(link, name)); err == nil {
				t.Fatal("unapproved linked source accepted")
			}
			// A once-valid path may not redirect an existing reference after issuance.
			folder := filepath.Join(ws, "moving")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			moving := filepath.Join(folder, name)
			if err := os.WriteFile(moving, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			old, err := a.OpenArtifact(id, moving)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(folder, folder+"-old"); err != nil {
				t.Fatal(err)
			}
			if kind == "junction" {
				cmd := exec.Command("cmd", "/c", "mklink", "/J", folder, outside)
				executil.HideConsoleWindow(cmd)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("retarget: %v %s", err, out)
				}
			} else if err := os.Symlink(outside, folder); err != nil {
				t.Fatal(err)
			}
			if got, err := a.ReadArtifact(old.Reference); err == nil || got != "" {
				t.Fatalf("retarget leaked: %q %v", got, err)
			}
			checkPreviewHTTP(t, "GET", old.URL, 403)
		})
	}
}

// Revocation must match previews the way grants were stored, or a relative or
// symlinked spelling leaves in-flight reads running after the user revoked them.
func TestRevokedDirectoryKeyMatchesStoredGrant(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "docs")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	stored, err := toolpath.Canonical(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got := revokedDirectoryKey(ws, "docs"); got != stored {
		t.Fatalf("relative revoke = %q, want %q", got, stored)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(sub, link); err == nil {
		if got := revokedDirectoryKey(ws, link); got != stored {
			t.Fatalf("symlinked revoke = %q, want %q", got, stored)
		}
	}
	gone := filepath.Join(ws, "removed")
	if got := revokedDirectoryKey(ws, "removed"); got != filepath.Clean(gone) {
		t.Fatalf("missing directory = %q, want %q", got, gone)
	}
}
