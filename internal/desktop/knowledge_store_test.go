package desktop

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeStoreRoundTripAndRejects(t *testing.T) {
	workspace := t.TempDir()
	id := "sess-store"
	scope := hex.EncodeToString(make([]byte, 32))
	disk := knowledgeDisk{
		Version:   knowledgeStoreVersionV2,
		SessionID: id,
		Selections: map[string]knowledgeSelection{
			scope: {
				Revision:   2,
				LibraryIDs: []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "demo:policies"},
				Summaries: []knowledgeSummary{
					{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Title: "制度", Description: "d"},
					{ID: "demo:policies", Title: "指南"},
				},
			},
		},
	}
	if err := writeKnowledge(workspace, id, disk); err != nil {
		t.Fatal(err)
	}
	got, err := readKnowledge(workspace, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != knowledgeStoreVersionV2 || got.Selections[scope].Revision != 2 || len(got.Selections[scope].LibraryIDs) != 2 {
		t.Fatalf("round-trip: %+v", got)
	}

	cases := []struct {
		name string
		mut  func(*knowledgeDisk)
	}{
		{"bad scope", func(d *knowledgeDisk) {
			d.Selections = map[string]knowledgeSelection{"not-hex": {LibraryIDs: []string{"demo:a"}}}
		}},
		{"too many libraries", func(d *knowledgeDisk) {
			ids := make([]string, knowledgeMaxSelection+1)
			for i := range ids {
				ids[i] = fmt.Sprintf("demo:x%d", i)
			}
			d.Selections[scope] = knowledgeSelection{LibraryIDs: ids}
		}},
		{"duplicate library", func(d *knowledgeDisk) {
			d.Selections[scope] = knowledgeSelection{LibraryIDs: []string{"demo:a", "demo:a"}}
		}},
		{"invalid library id", func(d *knowledgeDisk) {
			d.Selections[scope] = knowledgeSelection{LibraryIDs: []string{"not a uuid"}}
		}},
		{"summary for unknown id", func(d *knowledgeDisk) {
			d.Selections[scope] = knowledgeSelection{
				LibraryIDs: []string{"demo:a"},
				Summaries:  []knowledgeSummary{{ID: "demo:b", Title: "ghost"}},
			}
		}},
		{"empty summary title", func(d *knowledgeDisk) {
			d.Selections[scope] = knowledgeSelection{
				LibraryIDs: []string{"demo:a"},
				Summaries:  []knowledgeSummary{{ID: "demo:a", Title: "  "}},
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := knowledgeDisk{
				Version:    knowledgeStoreVersionV2,
				SessionID:  id + "-" + tc.name,
				Selections: map[string]knowledgeSelection{scope: {LibraryIDs: []string{"demo:a"}}},
			}
			tc.mut(&bad)
			raw, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, ".runcode", "knowledge", bad.SessionID+".json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readKnowledge(workspace, bad.SessionID); !errors.Is(err, errKnowledgeStorage) {
				t.Fatalf("err=%v, want storage", err)
			}
		})
	}
}

func TestValidateKnowledgeSummaries(t *testing.T) {
	if err := validateKnowledgeSummaries(knowledgeSelection{}); err != nil {
		t.Fatal(err)
	}
	sel := knowledgeSelection{
		LibraryIDs: []string{"demo:a"},
		Summaries:  []knowledgeSummary{{ID: "demo:a", Title: "ok"}},
	}
	if err := validateKnowledgeSummaries(sel); err != nil {
		t.Fatal(err)
	}
	sel.Summaries[0].ID = "demo:missing"
	if err := validateKnowledgeSummaries(sel); !errors.Is(err, errKnowledgeStorage) {
		t.Fatalf("err=%v", err)
	}
}
