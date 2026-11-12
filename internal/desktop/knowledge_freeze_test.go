package desktop

import (
	"testing"

	"github.com/wt68/runcode/internal/knowledgetool"
)

func TestKnowledgeFreezeLibrariesUsesLocalSummariesWithoutDroppingIDs(t *testing.T) {
	libA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	libB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	sel := knowledgeSelection{
		Revision:   3,
		LibraryIDs: []string{libA, libB, "demo:skip", "not-a-uuid"},
		Summaries: []knowledgeSummary{
			{ID: libA, Title: "制度指南", Description: "制度"},
		},
	}
	if got := knowledgeFreezeLibraries("demo", sel); got != nil {
		t.Fatalf("demo mode freeze = %#v, want nil", got)
	}
	got := knowledgeFreezeLibraries("real", sel)
	if len(got) != 2 {
		t.Fatalf("real freeze len=%d, want 2 valid UUIDs kept", len(got))
	}
	if got[0] != (knowledgetool.Overview{ID: libA, Title: "制度指南", Description: "制度"}) {
		t.Fatalf("libA = %#v", got[0])
	}
	if got[1].ID != libB || got[1].Title != libB {
		t.Fatalf("libB without summary should keep id as title: %#v", got[1])
	}
}
