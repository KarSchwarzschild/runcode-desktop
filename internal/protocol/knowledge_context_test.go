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
		{reflect.TypeOf(SubmitQuestionRequest{}), "KnowledgeOverride", "knowledgeOverride,omitempty"},
	} {
		field, ok := item.typ.FieldByName(item.name)
		if !ok || field.Tag.Get("json") != item.tag {
			t.Errorf("%s missing %s wire field", item.typ.Name(), item.name)
		}
	}
}
