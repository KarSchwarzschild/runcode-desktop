package protocol

import (
	"reflect"
	"testing"
)

func TestKnowledgeQuestionWireContract(t *testing.T) {
	for _, item := range []struct {
		typ       reflect.Type
		name, tag string
	}{
		{reflect.TypeOf(QuestionDraft{}), "Knowledge", "knowledge,omitempty"},
		{reflect.TypeOf(ResumedBlock{}), "Knowledge", "knowledge,omitempty"},
		{reflect.TypeOf(SubmitQuestionRequest{}), "KnowledgeRevision", "knowledgeRevision,omitempty"},
	} {
		field, ok := item.typ.FieldByName(item.name)
		if !ok || field.Tag.Get("json") != item.tag {
			t.Errorf("%s missing %s wire field", item.typ.Name(), item.name)
		}
	}
	if _, ok := reflect.TypeOf(SubmitQuestionRequest{}).FieldByName("KnowledgeOverride"); ok {
		t.Fatal("KnowledgeOverride must stay removed from SubmitQuestionRequest")
	}
	if _, ok := reflect.TypeOf(SubmitQuestionRequest{}).FieldByName("SubmissionID"); ok {
		t.Fatal("SubmissionID must stay removed from SubmitQuestionRequest")
	}
}
