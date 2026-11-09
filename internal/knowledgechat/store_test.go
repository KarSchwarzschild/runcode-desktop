package knowledgechat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testLibrary = "11111111-2222-4333-8444-555555555555"

func testDocument(t *testing.T) Document {
	t.Helper()
	return Document{Version: 1, Kind: Kind, ID: "kc-test", Scope: Scope{Workspace: t.TempDir(), Account: "issuer/user", Tenant: "tenant", KnowledgeBridge: "https://knowledge.test", ModelBridge: "https://model.test"}, Model: "model", Window: 8192, Output: 512, Selection: Selection{Revision: 1, LibraryIDs: []string{testLibrary}}}
}

func TestKnowledgeAcceptAtomic(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	s := NewStore(d.Scope.Workspace)
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	fail := errors.New("rename failed")
	s.beforeRename = func() error { return fail }
	if accepted, _, err := s.Accept(ctx, d.ID, d.Scope, SubmitInput{SubmissionID: "one", Text: "原问\n不改写", Revision: 1}); err == nil || accepted.Question.ID != "" {
		t.Fatalf("failed write accepted: %+v %v", accepted, err)
	}
	saved, err := s.Load(ctx, d.ID)
	if err != nil || len(saved.Questions) != 0 {
		t.Fatalf("failed write changed document: %+v %v", saved, err)
	}
	s.beforeRename = nil
	accepted, repeat, err := s.Accept(ctx, d.ID, d.Scope, SubmitInput{SubmissionID: "one", Text: "原问\n不改写", Revision: 1})
	if err != nil || repeat || accepted.Question.Status != Accepted || accepted.Question.Text != "原问\n不改写" || len(accepted.Question.Selection.LibraryIDs) != 1 {
		t.Fatalf("bad accept: %+v %v", accepted, err)
	}
	saved, err = s.Load(ctx, d.ID)
	if err != nil || len(saved.Questions) != 1 || saved.Questions[0].ID != accepted.Question.ID {
		t.Fatal("receipt not durable", err)
	}
	path := filepath.Join(d.Scope.Workspace, ".runcode", "knowledge-chat", d.ID+".json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Accept(ctx, d.ID, d.Scope, SubmitInput{SubmissionID: "two", Text: "next", Revision: 1}); err == nil {
		t.Fatal("corrupt history overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{broken" {
		t.Fatal("corrupt bytes changed")
	}
}

func TestKnowledgeSubmissionIdempotency(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	s := NewStore(d.Scope.Workspace)
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	in := SubmitInput{SubmissionID: "same", Text: "original", Revision: 1}
	first, _, err := s.Accept(ctx, d.ID, d.Scope, in)
	if err != nil {
		t.Fatal(err)
	}
	second, replayed, err := s.Accept(ctx, d.ID, d.Scope, in)
	if err != nil || !replayed || first.Question.ID != second.Question.ID {
		t.Fatal("duplicate accepted twice", err)
	}
	in.Text = "changed"
	if _, _, err := s.Accept(ctx, d.ID, d.Scope, in); !errors.Is(err, ErrConflict) {
		t.Fatal("same ID changed text", err)
	}
	in.SubmissionID = "new"
	if _, _, err := s.Accept(ctx, d.ID, d.Scope, in); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent acceptance", err)
	}
	wrong := d.Scope
	wrong.Account = "other"
	if _, _, err := s.Accept(ctx, d.ID, wrong, SubmitInput{SubmissionID: "same", Text: "original", Revision: 1}); !errors.Is(err, ErrScope) {
		t.Fatal("idempotency leaked receipt across accounts", err)
	}
}

func TestKnowledgeStoreRejectsUnknownFormat(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	s := NewStore(d.Scope.Workspace)
	d.Version = 2
	if err := s.Create(ctx, d); err == nil {
		t.Fatal("unknown version")
	}
	d.Version = 1
	d.Kind = "agent"
	if err := s.Create(ctx, d); err == nil {
		t.Fatal("unknown kind")
	}
	if _, err := s.Load(ctx, "../outside"); err == nil {
		t.Fatal("unsafe path")
	}
}
