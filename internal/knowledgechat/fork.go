package knowledgechat

import (
	"context"

	"github.com/google/uuid"
)

// Fork 只保存原题之前已完成的问答。未完成状态、原题及其后续内容不进入新分支。
// 新会话保留服务边界与原题选库；不会接受新问题，也不会自动检索或运行模型。
func (s *Store) Fork(ctx context.Context, id string, scope Scope, questionID string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load(ctx, id)
	if err != nil {
		return Document{}, err
	}
	if d.Scope != scope {
		return Document{}, ErrScope
	}
	index := -1
	for i, q := range d.Questions {
		if !Terminal(q.Status) {
			return Document{}, ErrBusy
		}
		if q.ID == questionID {
			index = i
		}
	}
	if index < 0 {
		return Document{}, ErrInput
	}
	fork := d
	fork.ID = "kc-" + uuid.NewString()
	fork.Parent = &Reference{SessionID: id, QuestionID: questionID}
	fork.Selection = d.Questions[index].Selection
	fork.Selection.LibraryIDs = append([]string{}, fork.Selection.LibraryIDs...)
	fork.Questions = []Question{}
	for _, q := range d.Questions[:index] {
		if q.Status == Completed {
			fork.Questions = append(fork.Questions, q)
		}
	}
	if err := s.save(ctx, fork); err != nil {
		return Document{}, err
	}
	return fork, nil
}

func sameReference(a, b *Reference) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func validReference(ref *Reference) bool {
	return ref == nil || validID(ref.SessionID) && ref.QuestionID != ""
}
