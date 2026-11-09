package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestKnowledgeAtomicRenameFailurePreservesConfirmedSelection(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sess.cwd, ".runcode", "knowledge", id+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Permit reads and writes, but deny replacing/deleting the existing file.
	handle, err := windows.CreateFile(pathUTF16, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, ExpectedRevision: 1}); err == nil {
		t.Fatal("failed atomic rename reported successful clear")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("atomic write failure changed confirmed disk state")
	}
	view, err := app.GetSessionKnowledge(id)
	if err != nil || view.Revision != 1 || len(view.Libraries) != 1 {
		t.Fatalf("failed write published unconfirmed state: %+v %v", view, err)
	}
	if err := app.SendMessage(id, "failed clear must still block"); err == nil {
		t.Fatal("failed clear bypassed knowledge gate")
	}
}

// Junctions can be created on Windows without the symlink privilege. Exercise
// the same workspace-escape boundary even on a normal desktop user account.
func TestKnowledgeStoreRejectsExternalWindowsJunction(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sess.cwd, ".runcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sess.cwd, ".runcode", "knowledge")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v %s", err, output)
	}
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}}); err == nil {
		t.Fatal("knowledge write escaped workspace via junction")
	}
	if _, err := app.GetSessionKnowledge(id); err == nil {
		t.Fatal("external junction read as empty selection")
	}
	if err := app.SendMessage(id, "must not execute via external junction"); err == nil {
		t.Fatal("external junction bypassed submission gate")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("knowledge state was written outside workspace")
	}
}
