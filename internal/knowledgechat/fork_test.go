package knowledgechat

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestKnowledgeFork(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	d.Selection = Selection{Revision: 4, LibraryIDs: []string{}}
	d.Questions = []Question{
		{ID: "complete", SubmissionID: "s1", Text: "先前问题", Status: Completed, Selection: Selection{Revision: 1, LibraryIDs: []string{testLibrary}}, Answer: "已保存回答", AffectedLibraryIDs: []string{testLibrary}},
		{ID: "failed", SubmissionID: "s2", Text: "失败问题", Status: Failed, Selection: Selection{Revision: 2}},
		{ID: "target", SubmissionID: "s3", Text: " 原题😀 ", Status: Cancelled, Selection: Selection{Revision: 3, LibraryIDs: []string{testLibrary}}},
		{ID: "after", SubmissionID: "s4", Text: "后续问题", Status: Completed, Selection: Selection{Revision: 4}, Answer: "后续回答"},
	}
	s := NewStore(d.Scope.Workspace)
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	fork, err := s.Fork(ctx, d.ID, d.Scope, "target")
	if err != nil {
		t.Fatal(err)
	}
	if fork.ID == d.ID || fork.Parent == nil || *fork.Parent != (Reference{SessionID: d.ID, QuestionID: "target"}) {
		t.Fatalf("分支身份或父引用错误: %+v", fork)
	}
	if len(fork.Questions) != 1 || fork.Questions[0].ID != "complete" || fork.Questions[0].Answer != "已保存回答" {
		t.Fatalf("分支复制了非完成或目标之后的历史: %+v", fork.Questions)
	}
	if !reflect.DeepEqual(fork.Selection, d.Questions[2].Selection) {
		t.Fatalf("未沿用原题选择: %+v", fork.Selection)
	}
	before, _ := s.Load(ctx, d.ID)
	if !reflect.DeepEqual(before, d) {
		t.Fatal("分支改变了来源会话")
	}
	empty := []string{}
	accepted, _, err := s.Accept(ctx, fork.ID, fork.Scope, SubmitInput{SubmissionID: "retry", Text: "修改后重试", Revision: 3, Parent: fork.Parent, LibraryIDs: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Question.ID == "target" || accepted.Question.Status != Accepted || len(accepted.Question.Selection.LibraryIDs) != 0 || accepted.Question.Parent == nil || *accepted.Question.Parent != *fork.Parent {
		t.Fatalf("重试未获得新身份或显式空选择失效: %+v", accepted.Question)
	}
	if !reflect.DeepEqual(accepted.Document.Selection, fork.Selection) {
		t.Fatal("逐题 override 改写了新会话当前选库")
	}
	if _, repeated, err := s.Accept(ctx, fork.ID, fork.Scope, SubmitInput{SubmissionID: "retry", Text: "修改后重试", Revision: 3, Parent: fork.Parent, LibraryIDs: &empty}); err != nil || !repeated {
		t.Fatalf("重试提交不幂等: %v", err)
	}
	if _, _, err := s.Accept(ctx, fork.ID, fork.Scope, SubmitInput{SubmissionID: "retry", Text: "修改后重试", Revision: 3, Parent: fork.Parent}); !errors.Is(err, ErrConflict) {
		t.Fatalf("幂等提交未比较选库: %v", err)
	}
}

func TestKnowledgeForkRejectsScopeAndLineageChanges(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	d.Questions = []Question{{ID: "target", SubmissionID: "s1", Text: "原题", Status: Failed, Selection: d.Selection}}
	s := NewStore(d.Scope.Workspace)
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	wrong := d.Scope
	wrong.Account = "other"
	if _, err := s.Fork(ctx, d.ID, wrong, "target"); !errors.Is(err, ErrScope) {
		t.Fatalf("跨账号分支: %v", err)
	}
	if _, err := s.Fork(ctx, d.ID, d.Scope, "missing"); err == nil {
		t.Fatal("允许不存在的父题")
	}
	fork, err := s.Fork(ctx, d.ID, d.Scope, "target")
	if err != nil {
		t.Fatal(err)
	}
	badParent := &Reference{SessionID: d.ID, QuestionID: "other"}
	if _, _, err := s.Accept(ctx, fork.ID, fork.Scope, SubmitInput{SubmissionID: "retry", Text: "重试", Revision: 1, Parent: badParent}); !errors.Is(err, ErrScope) {
		t.Fatalf("未验证分支父题: %v", err)
	}
	empty := []string{}
	if _, _, err := s.Accept(ctx, d.ID, d.Scope, SubmitInput{SubmissionID: "override", Text: "新问题", Revision: 1, LibraryIDs: &empty}); !errors.Is(err, ErrScope) {
		t.Fatalf("非分支可覆盖选择: %v", err)
	}
}

func TestKnowledgeSubmissionIdempotencyAfterSelectionChange(t *testing.T) {
	ctx := context.Background()
	d := testDocument(t)
	s := NewStore(d.Scope.Workspace)
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	in := SubmitInput{SubmissionID: "same", Text: "原文", Revision: 1}
	first, _, err := s.Accept(ctx, d.ID, d.Scope, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, d.ID, d.Scope, func(doc *Document) error { doc.Selection = Selection{Revision: 2, LibraryIDs: []string{}}; return nil }); err != nil {
		t.Fatal(err)
	}
	second, repeated, err := s.Accept(ctx, d.ID, d.Scope, in)
	if err != nil || !repeated || second.Question.ID != first.Question.ID {
		t.Fatalf("改标签后重复 IPC 丢失原 receipt: %+v %v", second, err)
	}
}

func TestKnowledgeStoreRejectsInvalidFrozenResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Document)
	}{
		{"oversize-answer", func(d *Document) { d.Questions[0].Answer = strings.Repeat("a", (64<<10)+1) }},
		{"invalid-source", func(d *Document) {
			d.Questions[0].Sources = []Source{{ID: "S1", LibraryID: "foreign", DocumentID: "doc", ChunkID: "chunk"}}
		}},
		{"untracked-source", func(d *Document) {
			d.Questions[0].Sources = []Source{{ID: "S1", LibraryID: testLibrary, DocumentID: "doc", ChunkID: "chunk"}}
		}},
		{"unverified-override", func(d *Document) { d.Questions[0].SelectionOverridden = true }},
		{"cumulative-libraries", func(d *Document) {
			for i := 0; i < 51; i++ {
				d.Questions = append(d.Questions, Question{ID: fmt.Sprint(i), SubmissionID: fmt.Sprint(i), Text: "问题", Status: Completed, Selection: Selection{}, AffectedLibraryIDs: []string{fmt.Sprintf("%08x-2222-4333-8444-555555555555", i+1)}})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDocument(t)
			d.Questions = []Question{{ID: "q", SubmissionID: "s", Text: "原问", Status: Completed, Selection: d.Selection, Answer: "回答"}}
			tc.change(&d)
			if err := NewStore(d.Scope.Workspace).Create(context.Background(), d); err == nil {
				t.Fatal("损坏的冻结回答进入了可恢复历史")
			}
		})
	}
}
