package knowledgetool

import (
	"context"
	"encoding/json"
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

func TestToolContractAndDirectDescription(t *testing.T) {
	h := &fakeHost{}
	tl := testTool(h, Overview{ID: libA, Title: "制度库", Description: "校内制度"})
	if tl.Name() != Name || tl.IsConcurrencySafe() {
		t.Fatal("contract")
	}
	schema, _ := json.Marshal(tl.InputSchema())
	if !strings.Contains(string(schema), `"query"`) || !strings.Contains(string(schema), "libraryIds") {
		t.Fatalf("schema=%s", schema)
	}
	if !strings.Contains(tl.Description(), libA) || !strings.Contains(tl.Description(), "制度库") {
		t.Fatalf("description=%s", tl.Description())
	}
	empty := testTool(h)
	if !strings.Contains(empty.Description(), "未选择") {
		t.Fatalf("empty desc=%s", empty.Description())
	}
}

func TestRunRejectsEmptySelectionAndEmptyLibraryIds(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h)
	tl.BeginQuestion("q1")
	res, err := tl.Run(context.Background(), json.RawMessage(`{"query":"制度"}`), &tool.Context{ToolUseID: "t1"}, nil)
	if err != nil || !res.IsError || h.calls.Load() != 0 {
		t.Fatalf("empty selection: %+v calls=%d", res, h.calls.Load())
	}
	tl = testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q2")
	res, err = tl.Run(context.Background(), json.RawMessage(`{"query":"制度","libraryIds":[]}`), &tool.Context{ToolUseID: "t2"}, nil)
	if err != nil || !res.IsError || h.calls.Load() != 0 {
		t.Fatalf("empty array: %+v", res)
	}
}

func TestRunSubsetOmitsAndRejectsForeign(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body text"}}}
	tl := testTool(h, Overview{ID: libA, Title: "A"}, Overview{ID: libB, Title: "B"})
	tl.BeginQuestion("q1")
	res, err := tl.Run(context.Background(), json.RawMessage(`{"query":"x","libraryIds":["`+libA+`"]}`), &tool.Context{ToolUseID: "t1"}, nil)
	if err != nil || res.IsError || h.calls.Load() != 1 {
		t.Fatalf("subset: %+v", res)
	}
	res, err = tl.Run(context.Background(), json.RawMessage(`{"query":"x","libraryIds":["`+libC+`"]}`), &tool.Context{ToolUseID: "t2"}, nil)
	if err != nil || !res.IsError || h.calls.Load() != 1 {
		t.Fatalf("foreign id must not retrieve: %+v calls=%d", res, h.calls.Load())
	}
}

func TestBudgetReplayAndLimit(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libA, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q1")
	first, _ := tl.Run(context.Background(), json.RawMessage(`{"query":"a"}`), &tool.Context{ToolUseID: "same"}, nil)
	second, _ := tl.Run(context.Background(), json.RawMessage(`{"query":"a"}`), &tool.Context{ToolUseID: "same"}, nil)
	if h.calls.Load() != 1 || first.Content[0].Text != second.Content[0].Text {
		t.Fatalf("replay must not re-HTTP: calls=%d", h.calls.Load())
	}
	_, _ = tl.Run(context.Background(), json.RawMessage(`{"query":"b"}`), &tool.Context{ToolUseID: "u2"}, nil)
	_, _ = tl.Run(context.Background(), json.RawMessage(`{"query":"c"}`), &tool.Context{ToolUseID: "u3"}, nil)
	limited, _ := tl.Run(context.Background(), json.RawMessage(`{"query":"d"}`), &tool.Context{ToolUseID: "u4"}, nil)
	if !limited.IsError || h.calls.Load() != 3 {
		t.Fatalf("limit: %+v calls=%d", limited, h.calls.Load())
	}
}

func TestUTF16QueryLimit(t *testing.T) {
	h := &fakeHost{}
	tl := testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q1")
	// U+1F600 is one rune but two UTF-16 code units.
	var b strings.Builder
	for i := 0; i < 2001; i++ {
		b.WriteRune('😀')
	}
	res, _ := tl.Run(context.Background(), mustJSON(map[string]string{"query": b.String()}), &tool.Context{ToolUseID: "t1"}, nil)
	if !res.IsError || h.calls.Load() != 0 {
		t.Fatalf("utf16 limit: %+v", res)
	}
}

func TestBoundHitsRejectsForeignLibrary(t *testing.T) {
	h := &fakeHost{hits: []RawHit{{LibraryID: libB, DocumentID: "d1", ChunkID: "c1", Title: "t", Text: "body"}}}
	tl := testTool(h, Overview{ID: libA, Title: "A"})
	tl.BeginQuestion("q1")
	res, _ := tl.Run(context.Background(), json.RawMessage(`{"query":"x"}`), &tool.Context{ToolUseID: "t1"}, nil)
	if !res.IsError {
		t.Fatalf("expected reject foreign hit: %+v", res)
	}
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
