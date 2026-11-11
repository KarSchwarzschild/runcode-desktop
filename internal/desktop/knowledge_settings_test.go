package desktop

import (
	"context"
	"testing"
)

// 同会话选库（无独立 e.chat）时，模式／思考／计划须走普通宿主入口，且不丢选择。
func TestKnowledgeSettingsApplyWithSelection(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, sess := openParallelSession(t, app, built)
	selected, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:employee-01"}, ExpectedRevision: 0})
	if err != nil || selected.Revision != 1 || len(selected.Libraries) != 1 {
		t.Fatalf("save: %+v %v", selected, err)
	}
	if err := app.SetPermissionMode(id, "interactive"); err != nil {
		t.Fatalf("permission: %v", err)
	}
	if _, err := app.SetPlanMode(id, true); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := app.SetThinkingEffort(id, "high"); err != nil {
		t.Fatalf("thinking: %v", err)
	}
	if _, err := app.SetReasoningScenario(id, "auto"); err != nil {
		t.Fatalf("reasoning: %v", err)
	}
	if !sess.planMode || sess.thinkingEffort != "high" || sess.permissionMode != "interactive" || sess.reasoningScenario != "auto" {
		t.Fatalf("host session settings not applied: %+v", sess.Status())
	}
	info, err := app.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if !info.PlanMode || info.ThinkingEffort != "high" || info.PermissionMode != "interactive" {
		t.Fatalf("status not applied: %+v", info)
	}
	kept, err := app.GetSessionKnowledge(id)
	if err != nil || kept.Revision != 1 || len(kept.Libraries) != 1 || kept.Libraries[0].ID != "demo:employee-01" {
		t.Fatalf("settings cleared selection: %+v %v", kept, err)
	}
}

func TestKnowledgeModelSwitchPreservesSelection(t *testing.T) {
	enableKnowledgeDemo(t)
	app, _, built := newParallelApp(t)
	defer func() { _ = app.mgr.CloseAll(context.Background()) }()
	id, _ := openParallelSession(t, app, built)
	if _, err := app.SetSessionKnowledge(SetSessionKnowledgeRequest{SessionID: id, LibraryIDs: []string{"demo:policies"}, ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	// SetModel 走重建；旧 fake 指针会失效，以落盘选择与新 Status 为准。
	if err := app.SetModel(id, "fake-next"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	info, err := app.Status(id)
	if err != nil || info.Model != "fake-next" {
		t.Fatalf("status model: %+v %v", info, err)
	}
	kept, err := app.GetSessionKnowledge(id)
	if err != nil || kept.Revision != 1 || len(kept.Libraries) != 1 || kept.Libraries[0].ID != "demo:policies" {
		t.Fatalf("model path lost selection: %+v %v", kept, err)
	}
}
