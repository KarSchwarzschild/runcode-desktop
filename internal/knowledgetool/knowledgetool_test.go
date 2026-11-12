package knowledgetool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
)

const (
	libA = "11111111-1111-1111-1111-111111111111"
	libB = "22222222-2222-2222-2222-222222222222"
	libC = "33333333-3333-3333-3333-333333333333"
)

type fakeHost struct {
	calls atomic.Int32
	hits  []RawHit
	err   error
}

func (h *fakeHost) Retrieve(_ context.Context, _ string, _ []string, _ CallMeta) ([]RawHit, string, error) {
	h.calls.Add(1)
	if h.err != nil {
		return nil, "", h.err
	}
	status := "empty"
	if len(h.hits) > 0 {
		status = "matched"
	}
	return append([]RawHit(nil), h.hits...), status, nil
}

func testTool(h *fakeHost, libs ...Overview) *Tool {
	snap := Snapshot{QuestionID: "q1", Libraries: libs}
	return &Tool{
		Host:         h,
		SnapshotFunc: func() Snapshot { return snap },
	}
}

func resultText(out tool.Result) string {
	var b strings.Builder
	for _, part := range out.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestParseInputRejectsIDOutsideSnapshot(t *testing.T) {
	snap := Snapshot{Libraries: []Overview{{ID: libA, Title: "A"}, {ID: libB, Title: "B"}}}
	_, _, err := parseInput(mustJSON(map[string]any{"query": "x", "libraryIds": []string{libC}}), snap)
	if err == "" || !strings.Contains(err, "超出本题已选范围") {
		t.Fatalf("err=%q, want 超出本题已选范围", err)
	}
	query, ids, err := parseInput(mustJSON(map[string]any{"query": "x", "libraryIds": []string{libA}}), snap)
	if err != "" || query != "x" || len(ids) != 1 || ids[0] != libA {
		t.Fatalf("subset parse: query=%q ids=%v err=%q", query, ids, err)
	}
	_, all, err := parseInput(mustJSON(map[string]any{"query": "x"}), snap)
	if err != "" || len(all) != 2 {
		t.Fatalf("omit libraryIds: ids=%v err=%q", all, err)
	}
}

func TestBoundHitsRejectsForeignLibraryHit(t *testing.T) {
	allowed := map[string]bool{libA: true}
	_, _, err := boundHits([]RawHit{{
		LibraryID: libB, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body",
	}}, allowed)
	if err == "" || !strings.Contains(err, "越权") {
		t.Fatalf("err=%q, want 越权", err)
	}
	hits, truncated, err := boundHits([]RawHit{{
		LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body",
	}}, allowed)
	if err != "" || truncated || len(hits) != 1 || hits[0].LibraryID != libA {
		t.Fatalf("allowed hit: %+v truncated=%v err=%q", hits, truncated, err)
	}
}

func TestBeginQuestionResetsBudgetAcrossTurnsWithEmptyID(t *testing.T) {
	h := &fakeHost{}
	tl := testTool(h, Overview{ID: libA, Title: "lib"})
	input := mustJSON(map[string]any{"query": "ping"})
	// Same empty id every host freeze — skipping same-id used to leave the
	// MaxCallsPerTurn counter stuck for the rest of the session.
	for turn := 0; turn < 2; turn++ {
		tl.BeginQuestion("")
		for i := 0; i < MaxCallsPerTurn; i++ {
			out, err := tl.Run(context.Background(), input, &tool.Context{ToolUseID: fmt.Sprintf("ok-%d-%d", turn, i)}, nil)
			if err != nil || out.IsError {
				t.Fatalf("turn %d call %d: err=%v out=%s", turn, i, err, resultText(out))
			}
		}
		out, err := tl.Run(context.Background(), input, &tool.Context{ToolUseID: fmt.Sprintf("over-%d", turn)}, nil)
		if err != nil || !out.IsError || !strings.Contains(resultText(out), "已达上限") {
			t.Fatalf("turn %d over-budget = %#v", turn, out)
		}
	}
	if h.calls.Load() != int32(2*MaxCallsPerTurn) {
		t.Fatalf("retrieves = %d, want %d", h.calls.Load(), 2*MaxCallsPerTurn)
	}
}

func TestBudgetReplayAndLimit(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q1")
	first, _ := tl.Run(context.Background(), mustJSON(map[string]string{"query": "a"}), &tool.Context{ToolUseID: "same"}, nil)
	second, _ := tl.Run(context.Background(), mustJSON(map[string]string{"query": "a"}), &tool.Context{ToolUseID: "same"}, nil)
	if h.calls.Load() != 1 || resultText(first) != resultText(second) {
		t.Fatalf("replay must not re-HTTP: calls=%d", h.calls.Load())
	}
	_, _ = tl.Run(context.Background(), mustJSON(map[string]string{"query": "b"}), &tool.Context{ToolUseID: "u2"}, nil)
	_, _ = tl.Run(context.Background(), mustJSON(map[string]string{"query": "c"}), &tool.Context{ToolUseID: "u3"}, nil)
	limited, _ := tl.Run(context.Background(), mustJSON(map[string]string{"query": "d"}), &tool.Context{ToolUseID: "u4"}, nil)
	if !limited.IsError || h.calls.Load() != 3 {
		t.Fatalf("limit: %+v calls=%d", limited, h.calls.Load())
	}
}

func TestRunRejectsEmptySelectionAndForeignLibraryID(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h)
	tl.BeginQuestion("q1")
	res, err := tl.Run(context.Background(), mustJSON(map[string]string{"query": "制度"}), &tool.Context{ToolUseID: "t1"}, nil)
	if err != nil || !res.IsError || h.calls.Load() != 0 {
		t.Fatalf("empty selection: %+v calls=%d", res, h.calls.Load())
	}
	tl = testTool(h, Overview{ID: libA, Title: "A"}, Overview{ID: libB, Title: "B"})
	tl.BeginQuestion("q2")
	res, err = tl.Run(context.Background(), mustJSON(map[string]any{"query": "x", "libraryIds": []string{libC}}), &tool.Context{ToolUseID: "t2"}, nil)
	if err != nil || !res.IsError || h.calls.Load() != 0 {
		t.Fatalf("foreign id must not retrieve: %+v calls=%d", res, h.calls.Load())
	}
}

func TestRunRejectsForeignHitMixedIntoResults(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libB, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q1")
	res, _ := tl.Run(context.Background(), mustJSON(map[string]string{"query": "x"}), &tool.Context{ToolUseID: "t1"}, nil)
	if !res.IsError || !strings.Contains(resultText(res), "越权") {
		t.Fatalf("expected reject foreign hit: %s", resultText(res))
	}
}
