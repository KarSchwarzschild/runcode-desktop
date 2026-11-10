package desktop

import (
	"encoding/json"

	"github.com/wt68/runcode/internal/imagegentool"
	appwire "github.com/wt68/runcode/internal/protocol"
)

// imageGenerationBlocks attaches image_generate results to a reopened
// conversation. The session's ledger is authoritative (final status and any
// recorded accounting observation); the persisted tool content is the
// fallback — a fork has the source's history prefix but no ledger of its own.
// A ledger record whose tool result never reached history (the process stopped
// after saving) is added once under its original question. Replay never
// submits anything: an interrupted submission shows as unknown.
func imageGenerationBlocks(workspace, sessionID string, blocks []ResumedBlock) []ResumedBlock {
	records, err := listImageGenerationLedgers(workspace, sessionID)
	if err != nil {
		debugLog("image generation ledger for %s unreadable: %v", sessionID, err)
		records = nil
	}
	byToolUse := make(map[string]imageGenerationLedger, len(records))
	for _, rec := range records {
		if rec.ToolUseID != "" {
			byToolUse[rec.ToolUseID] = rec
		}
	}
	seen := map[string]bool{}
	questions := map[string]bool{}
	for i := range blocks {
		if blocks[i].Kind == "user" && blocks[i].QuestionID != "" {
			questions[blocks[i].QuestionID] = true
		}
		t := blocks[i].Tool
		if blocks[i].Kind != "tool" || t == nil || t.ToolName != imagegentool.Name {
			continue
		}
		seen[t.ToolUseID] = true
		if rec, ok := byToolUse[t.ToolUseID]; ok {
			result := rec.effectiveResult()
			t.ImageGeneration = &result
		} else if result, ok := imageGenerationResultFromContent(t.Output); ok {
			t.ImageGeneration = &result
		}
	}

	recovered := map[string][]ResumedBlock{}
	var orphans []ResumedBlock
	for _, rec := range records {
		toolUseID := rec.ToolUseID
		if toolUseID == "" {
			toolUseID = "image_generation_" + rec.GenerationID
		}
		if seen[toolUseID] {
			continue
		}
		result := rec.effectiveResult()
		block := ResumedBlock{Kind: "tool", Tool: &ResumedTool{
			ToolName: imagegentool.Name, ToolUseID: toolUseID, IsError: result.Status != "succeeded",
			Output: "已从生图账本恢复：" + result.Status, ImageGeneration: &result,
		}}
		if questions[rec.QuestionID] {
			recovered[rec.QuestionID] = append(recovered[rec.QuestionID], block)
		} else {
			orphans = append(orphans, block)
		}
	}
	if len(recovered) == 0 && len(orphans) == 0 {
		return blocks
	}
	out := make([]ResumedBlock, 0, len(blocks)+len(records))
	question := ""
	for _, b := range blocks {
		if b.Kind == "user" {
			out = append(out, recovered[question]...)
			question = b.QuestionID
		}
		out = append(out, b)
	}
	out = append(out, recovered[question]...)
	return append(out, orphans...)
}

// imageGenerationResultFromContent reads the bounded result JSON the tool
// returned to the model (the same document the live card rendered).
func imageGenerationResultFromContent(content string) (appwire.ImageGenerationResult, bool) {
	var r appwire.ImageGenerationResult
	if json.Unmarshal([]byte(content), &r) != nil || r.Version != 1 || r.Status == "" {
		return appwire.ImageGenerationResult{}, false
	}
	if r.Images == nil || r.Status != "succeeded" {
		r.Images = []appwire.ImageGenerationArtifact{}
	}
	r.AccountingStatus = normalizeAccountingStatus(r.AccountingStatus)
	return r, true
}
