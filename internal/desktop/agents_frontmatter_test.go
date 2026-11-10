package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentMetadataRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	a := &App{workspace: t.TempDir()}
	for _, tools := range []string{"Read, Grep", "*", "", "null"} {
		want := "Review: # code\n\nKeep paragraphs.\n"
		list, err := a.SaveAgent(AgentSaveRequest{Scope: "project", Name: "custom-reviewer", Description: want, Tools: tools, Model: "model: variant", Prompt: "instructions"})
		if err != nil || len(list.Problems) != 0 {
			t.Fatalf("save tools=%q: %#v, %v", tools, list, err)
		}
		found := false
		for _, ag := range list.Agents {
			if ag.Name != "custom-reviewer" {
				continue
			}
			found = true
			if ag.Description != want || ag.Tools != tools || ag.Model != "model: variant" || ag.Prompt != "instructions\n" {
				t.Fatalf("agent round trip changed values: %#v", ag)
			}
		}
		if !found {
			t.Fatal("saved agent was not loaded")
		}
	}
}

func TestAgentImportSharesMetadataSyntax(t *testing.T) {
	isolateConfigDir(t)
	for _, header := range []string{
		"name: imported-reviewer\ndescription: >-\n  Review code\n  carefully.\ntools: Read, Grep\nmetadata:\n  name: not-the-agent\n",
		"name: imported-reviewer\ndescription: Review: code\ntools: *\n",
	} {
		path := filepath.Join(t.TempDir(), "source.md")
		if err := os.WriteFile(path, []byte("---\n"+header+"---\nPrompt"), 0o600); err != nil {
			t.Fatal(err)
		}
		a := &App{workspace: t.TempDir(), dialog: fakeDialoger{path: path}}
		list, err := a.ImportAgent("project")
		if err != nil || len(list.Problems) != 0 {
			t.Fatalf("import: %#v, %v", list, err)
		}
		found := false
		for _, ag := range list.Agents {
			if ag.Name == "imported-reviewer" {
				found = true
				if ag.Description != "Review code carefully." && ag.Description != "Review: code" {
					t.Fatalf("imported description = %q", ag.Description)
				}
			}
		}
		if !found {
			t.Fatal("imported agent not loaded")
		}
	}
}
