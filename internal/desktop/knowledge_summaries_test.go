package desktop

import (
	"reflect"
	"testing"
)

func TestKnowledgeDetailsFromSummariesKeepsRetainedTitles(t *testing.T) {
	summaries := []knowledgeSummary{
		{ID: "a", Title: "制度指南", Description: "d1"},
		{ID: "b", Title: "产品手册", Description: "d2"},
		{ID: "c", Title: knowledgeUnavailableTitle},
	}
	got := knowledgeDetailsFromSummaries(summaries, []string{"a", "c"})
	if len(got) != 1 || got["a"].Title != "制度指南" {
		t.Fatalf("got %#v, want only retained real title a", got)
	}
}

func TestKnowledgeSummariesForPersistDoesNotWritePlaceholders(t *testing.T) {
	previous := []knowledgeSummary{
		{ID: "a", Title: "制度指南", Description: "d1"},
		{ID: "b", Title: "产品手册", Description: "d2"},
	}
	// Remove-only view marked retained libs unavailable.
	view := []KnowledgeBaseSummary{
		{ID: "a", Title: knowledgeUnavailableTitle, UnavailableReason: "offline"},
	}
	got := knowledgeSummariesForPersist(previous, view)
	want := []knowledgeSummary{{ID: "a", Title: "制度指南", Description: "d1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
