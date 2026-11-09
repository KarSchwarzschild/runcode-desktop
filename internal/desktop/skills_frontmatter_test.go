package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillBlockDescriptionsRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	src := t.TempDir()
	content := "---\nname: folded\ndescription: >-\n  Use for: slides\n  and outlines.\n\n  Keep # labels.\ndisplay-description: |-\n  第一段\n  第二段\n---\n\n# Body\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{workspace: t.TempDir(), dialog: fakeDialoger{path: src}}
	list, err := a.ImportSkill("project")
	if err != nil || len(list.Skills) != 1 || len(list.Problems) != 0 {
		t.Fatalf("import: %#v, %v", list, err)
	}
	got := list.Skills[0]
	want := "Use for: slides and outlines.\nKeep # labels."
	if got.Description != want || got.DisplayDescription != "第一段\n第二段" {
		t.Fatalf("block descriptions = %#v", got)
	}
	list, err = a.SaveSkill(SkillSaveRequest{Scope: "project", Name: got.Name, Description: got.Description, DisplayDescription: got.DisplayDescription, Body: got.Body})
	if err != nil || len(list.Skills) != 1 || len(list.Problems) != 0 {
		t.Fatalf("save: %#v, %v", list, err)
	}
	if after := list.Skills[0]; after.Description != got.Description || after.DisplayDescription != got.DisplayDescription || after.Body != got.Body {
		t.Fatalf("save changed the values: before=%#v after=%#v", got, after)
	}
}

func TestSkillSpecialCharacterDescriptionsRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	a := &App{workspace: t.TempDir()}
	for _, description := range []string{">-", "|", "null", "true", "001", `Use for: "slides" # literal C:\tmp`, "第一段\n\n第二段\n\n"} {
		list, err := a.SaveSkill(SkillSaveRequest{
			Scope: "project", Name: "special", Description: description,
			DisplayName: `名称: "标题" # 注释`, DisplayDescription: "人类简介: # 原样\n第二行", Body: "body",
		})
		if err != nil || len(list.Skills) != 1 || len(list.Problems) != 0 {
			t.Fatalf("save %q: %#v, %v", description, list, err)
		}
		got := list.Skills[0]
		if got.Description != description || got.DisplayName != `名称: "标题" # 注释` || got.DisplayDescription != "人类简介: # 原样\n第二行" {
			t.Fatalf("save %q changed metadata: %#v", description, got)
		}
	}
}

func TestMarketMetadataPreservesExistingDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := string([]byte{0xEF, 0xBB, 0xBF}) + "---\r\nname: sample\r\ndescription: >-\r\n  Use for: slides\r\n  and outlines.\r\ndisplay_name: >-\r\n  包内\r\n  名称\r\nmetadata:\r\n  nested: retained\r\n---\r\n\r\n# Body\r\n---\r\nname: not metadata\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	wantDesc := "中文简介: # 保留\n下一行"
	if err := setSkillDisplayMeta(dir, "不能覆盖", wantDesc); err != nil {
		t.Fatal(err)
	}
	name, desc := skillDisplayMeta(path)
	if name != "包内 名称" || desc != wantDesc {
		t.Fatalf("display metadata = %q, %q", name, desc)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	firstEnd := strings.IndexByte(original, '\n') + 1
	if !strings.HasPrefix(string(after), original[:firstEnd]) || !strings.HasSuffix(string(after), original[firstEnd:]) {
		t.Fatalf("market insertion rewrote original bytes: %q", after)
	}
	if got := frontmatterValue(string(after), "description"); got != "Use for: slides and outlines." {
		t.Fatalf("model description changed: %q", got)
	}
	if err := setSkillDisplayMeta(dir, "覆盖名字", "覆盖描述"); err != nil {
		t.Fatal(err)
	}
	if again, err := os.ReadFile(path); err != nil || string(again) != string(after) {
		t.Fatalf("second insertion was not a no-op: %v", err)
	}
}

func TestMarketMetadataRejectsBrokenYAMLWithoutWriting(t *testing.T) {
	for _, header := range []string{"description: [broken", "description: >-\nunindented", "name: duplicate\nname: duplicate"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "SKILL.md")
		original := "---\n" + header + "\n---\nbody"
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setSkillDisplayMeta(dir, "name", "desc"); err == nil {
			t.Errorf("accepted broken metadata: %q", header)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != original {
			t.Fatalf("failed insertion changed file: %q, %v", after, err)
		}
	}
}

func TestMarketMetadataDoesNotDuplicateDeclaredEmptyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: empty\ndescription: d\ndisplay_name:\ndisplay_description: ''\n---\nbody"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setSkillDisplayMeta(dir, "name", "desc"); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != original {
		t.Fatalf("explicit empty fields were duplicated: %q, %v", after, err)
	}
}
