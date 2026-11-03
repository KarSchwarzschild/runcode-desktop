package desktop

import (
	"context"
	"encoding/json"
	"testing"

	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
)

func TestOALockCoversEveryWebFetchRoute(t *testing.T) {
	locked := ""
	policy := newOALockPolicy(permissions.DefaultPolicy{}, func() string { return locked })
	for _, route := range []string{"auto", "configured", "http://127.0.0.1:7890"} {
		for _, diagnose := range []bool{false, true} {
			input, _ := json.Marshal(map[string]any{"url": "https://example.com", "proxy": route, "diagnose_only": diagnose})
			action, err := (permissions.DefaultResolver{}).Resolve(context.Background(), permissions.ResolveRequest{ToolName: "WebFetch", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			locked = ""
			if decision := policy.Decide(context.Background(), action); decision.Effect != permissions.EffectAsk {
				t.Fatalf("unlocked network bypassed approval: %+v", decision)
			}
			locked = "local-model"
			if decision := policy.Decide(context.Background(), action); decision.Effect != permissions.EffectDeny || decision.Reason != reasonOALocked {
				t.Fatalf("route bypassed OA lock: %+v", decision)
			}
		}
	}
}
