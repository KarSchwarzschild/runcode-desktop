package desktop

import (
	"context"
	"testing"

	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
)

func TestSVGWritePolicyAllowsAskForSVGAnywhere(t *testing.T) {
	inner := permissions.DefaultPolicy{}
	p := newSVGWritePolicy(inner)

	cases := []permissions.Action{
		{
			ToolName:  "Write",
			Operation: permissions.OperationWrite,
			Risk:      permissions.RiskHigh,
			Resources: []permissions.Resource{{Type: permissions.ResourceFile, Scope: permissions.ResourceScopeWorkspace, Path: `/ws/simple.svg`}},
		},
		{
			ToolName:  "Write",
			Operation: permissions.OperationWrite,
			Risk:      permissions.RiskHigh,
			Resources: []permissions.Resource{{Type: permissions.ResourceFile, Scope: permissions.ResourceScopeOutside, Path: `D:\tmp\out.SVG`}},
		},
		{
			ToolName:  "Edit",
			Operation: permissions.OperationEdit,
			Risk:      permissions.RiskHigh,
			Resources: []permissions.Resource{{Type: permissions.ResourceFile, Scope: permissions.ResourceScopeWorkspace, Path: `/ws/icon.svg`}},
			Metadata:  map[string]any{permissions.MetadataReadState: permissions.ReadStateFresh},
		},
	}
	for _, action := range cases {
		d := p.Decide(context.Background(), action)
		if d.FinalEffect != permissions.EffectAllow || d.Reason != reasonSVGWrite {
			t.Fatalf("action %+v => %+v, want allow svg_write", action, d)
		}
	}
}

func TestSVGWritePolicyLeavesNonSVGAndDeniesAlone(t *testing.T) {
	inner := permissions.DefaultPolicy{}
	p := newSVGWritePolicy(inner)

	png := permissions.Action{
		ToolName:  "Write",
		Operation: permissions.OperationWrite,
		Risk:      permissions.RiskHigh,
		Resources: []permissions.Resource{{Type: permissions.ResourceFile, Scope: permissions.ResourceScopeWorkspace, Path: `/ws/a.png`}},
	}
	if d := p.Decide(context.Background(), png); d.FinalEffect != permissions.EffectAsk {
		t.Fatalf("png write should still ask, got %+v", d)
	}

	// 覆盖已存在文件且未读：内层硬拒，外层不得改成放行。
	exists := permissions.Action{
		ToolName:  "Write",
		Operation: permissions.OperationWrite,
		Risk:      permissions.RiskHigh,
		Resources: []permissions.Resource{{Type: permissions.ResourceFile, Scope: permissions.ResourceScopeWorkspace, Path: `/ws/simple.svg`}},
		Metadata: map[string]any{
			permissions.MetadataReadState:    permissions.ReadStateMissing,
			permissions.MetadataMutationKind: permissions.MutationKindOverwrite,
		},
	}
	if d := p.Decide(context.Background(), exists); d.FinalEffect != permissions.EffectDeny {
		t.Fatalf("overwrite without read must stay deny, got %+v", d)
	}
}

func TestIsSVGPath(t *testing.T) {
	if !isSVGPath(`C:\a\b.SVG`) || !isSVGPath("x.svg") {
		t.Fatal("expected .svg match")
	}
	if isSVGPath("x.svg.bak") || isSVGPath("svg") || isSVGPath("") {
		t.Fatal("unexpected .svg match")
	}
}
