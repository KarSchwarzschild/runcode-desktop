package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wt68/runcode/internal/imagegentool"
	appwire "github.com/wt68/runcode/internal/protocol"
)

func replayResult(genID, status string) appwire.ImageGenerationResult {
	r := appwire.ImageGenerationResult{Version: 1, GenerationID: genID, Status: status, Model: "image-only-b64", Size: "1024x1024", AccountingStatus: "unverified", Images: []appwire.ImageGenerationArtifact{}}
	if status == "succeeded" {
		r.Images = []appwire.ImageGenerationArtifact{{Path: "生成图片/" + genID + "/image.png", MIMEType: "image/png", Bytes: 70, Width: 1, Height: 1}}
	}
	return r
}

func toolBlock(toolUseID string, result appwire.ImageGenerationResult) ResumedBlock {
	raw, _ := json.Marshal(result)
	return ResumedBlock{Kind: "tool", Tool: &ResumedTool{ToolName: imagegentool.Name, ToolUseID: toolUseID, Output: string(raw), IsError: result.Status != "succeeded"}}
}

func writeReplayLedger(t *testing.T, ws, sessionID, questionID, toolUseID, genID, status, accounting string) {
	t.Helper()
	rec := imageGenerationLedger{GenerationID: genID, SessionID: sessionID, TurnID: "turn_" + genID, QuestionID: questionID, ToolUseID: toolUseID, Model: "image-only-b64", Size: "1024x1024", Status: "submitted", AccountingStatus: accounting}
	if status != "submitted" {
		rec = rec.finalized(replayResult(genID, status))
		rec.RequestID = "brg-" + genID
		rec.Result.RequestID = rec.RequestID
	}
	if err := writeImageGenerationLedger(ws, rec); err != nil {
		t.Fatal(err)
	}
}

func TestImageGenerationReplayMergesLedgerByToolUseID(t *testing.T) {
	ws := t.TempDir()
	writeReplayLedger(t, ws, "sess_1", "q_1", "tu_1", "gen_1", "succeeded", "verified")
	blocks := []ResumedBlock{
		{Kind: "user", QuestionID: "q_1", Text: "生成一张海报"},
		toolBlock("tu_1", replayResult("gen_1", "succeeded")),
		{Kind: "assistant", Text: "好的"},
	}
	out := imageGenerationBlocks(ws, "sess_1", blocks)
	if len(out) != 3 {
		t.Fatalf("blocks duplicated: %d", len(out))
	}
	got := out[1].Tool.ImageGeneration
	if got == nil || got.Status != "succeeded" || got.AccountingStatus != "verified" || got.RequestID != "brg-gen_1" || len(got.Images) != 1 {
		t.Fatalf("merged = %+v", got)
	}
}

func TestImageGenerationReplayRecoversUnwrittenToolUnderOriginalQuestion(t *testing.T) {
	ws := t.TempDir()
	// The image was saved and the ledger finalized, but the process stopped before
	// the tool result reached history.
	writeReplayLedger(t, ws, "sess_1", "q_1", "tu_1", "gen_1", "succeeded", "")
	writeReplayLedger(t, ws, "sess_1", "q_2", "tu_2", "gen_2", "submitted", "")
	blocks := []ResumedBlock{
		{Kind: "user", QuestionID: "q_1", Text: "第一题"},
		{Kind: "assistant", Text: "回答一"},
		{Kind: "user", QuestionID: "q_2", Text: "第二题"},
		{Kind: "user", QuestionID: "q_3", Text: "第三题"},
	}
	out := imageGenerationBlocks(ws, "sess_1", blocks)
	kinds := []string{}
	for _, b := range out {
		k := b.Kind
		if b.Tool != nil {
			k += ":" + b.Tool.ToolUseID + ":" + b.Tool.ImageGeneration.Status
		}
		kinds = append(kinds, k)
	}
	want := "user|assistant|tool:tu_1:succeeded|user|tool:tu_2:unknown|user"
	if strings.Join(kinds, "|") != want {
		t.Fatalf("order = %s, want %s", strings.Join(kinds, "|"), want)
	}
	unknown := out[4].Tool.ImageGeneration
	if !strings.Contains(unknown.Message, "可能已生成") || unknown.AccountingStatus != "unverified" || !out[4].Tool.IsError {
		t.Fatalf("interrupted submission = %+v", unknown)
	}
	again := imageGenerationBlocks(ws, "sess_1", out)
	if len(again) != len(out) {
		t.Fatal("replaying twice inserted the recovered card again")
	}
}

func TestImageGenerationReplayWithoutLedgerUsesToolContent(t *testing.T) {
	// A fork has the source's history prefix but no ledger of its own.
	blocks := []ResumedBlock{{Kind: "user", QuestionID: "q_1"}, toolBlock("tu_1", replayResult("gen_1", "save_failed"))}
	out := imageGenerationBlocks(t.TempDir(), "fork_1", blocks)
	if got := out[1].Tool.ImageGeneration; got == nil || got.Status != "save_failed" || len(got.Images) != 0 || got.AccountingStatus != "unverified" {
		t.Fatalf("content fallback = %+v", got)
	}
	bogus := []ResumedBlock{{Kind: "tool", Tool: &ResumedTool{ToolName: imagegentool.Name, ToolUseID: "x", Output: "not json"}}}
	if imageGenerationBlocks(t.TempDir(), "fork_1", bogus)[0].Tool.ImageGeneration != nil {
		t.Fatal("unparseable content produced a card")
	}
}

func TestImageGenerationReplayToleratesDamagedLedger(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".runcode", "image-generation", "requests", "sess_1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gen_1.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks := []ResumedBlock{{Kind: "user", QuestionID: "q_1"}, toolBlock("tu_1", replayResult("gen_1", "succeeded"))}
	out := imageGenerationBlocks(ws, "sess_1", blocks)
	if len(out) != 2 || out[1].Tool.ImageGeneration == nil || out[1].Tool.ImageGeneration.Status != "succeeded" {
		t.Fatalf("damaged ledger broke history: %+v", out)
	}
}
