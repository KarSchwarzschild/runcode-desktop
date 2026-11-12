package knowledgetool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

type stubHost struct {
	n int
}

func (h *stubHost) Retrieve(context.Context, string, []string, CallMeta) ([]RawHit, string, error) {
	h.n++
	return nil, "empty", nil
}

func resultText(out tool.Result) string {
	var b strings.Builder
	for _, part := range out.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

func TestBeginQuestionResetsBudgetAcrossTurnsWithEmptyID(t *testing.T) {
	host := &stubHost{}
	lib := "11111111-1111-1111-1111-111111111111"
	tl := &Tool{
		Host: host,
		SnapshotFunc: func() Snapshot {
			return Snapshot{Libraries: []Overview{{ID: lib, Title: "lib"}}}
		},
	}
	input, err := json.Marshal(map[string]any{"query": "ping"})
	if err != nil {
		t.Fatal(err)
	}

	// Same empty id every host freeze — the previous early-return left the
	// counter stuck after MaxCallsPerTurn for the rest of the session.
	for turn := 0; turn < 2; turn++ {
		tl.BeginQuestion("")
		for i := 0; i < MaxCallsPerTurn; i++ {
			out, err := tl.Run(context.Background(), input, &tool.Context{ToolUseID: fmt.Sprintf("ok-%d-%d", turn, i)}, nil)
			if err != nil {
				t.Fatalf("turn %d call %d: %v", turn, i, err)
			}
			if out.IsError {
				t.Fatalf("turn %d call %d unexpectedly failed: %s", turn, i, resultText(out))
			}
		}
		out, err := tl.Run(context.Background(), input, &tool.Context{ToolUseID: fmt.Sprintf("over-%d", turn)}, nil)
		if err != nil {
			t.Fatalf("turn %d over-budget: %v", turn, err)
		}
		if !out.IsError || !strings.Contains(resultText(out), "已达上限") {
			t.Fatalf("turn %d over-budget = %#v, want 已达上限", turn, out)
		}
	}
	if host.n != 2*MaxCallsPerTurn {
		t.Fatalf("retrieves = %d, want %d", host.n, 2*MaxCallsPerTurn)
	}
}
