package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wt68/runcode/internal/imagegentool"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
)

// imageGenerationResolver only adds display text to image_generate approvals:
// which model, tenant, size and prompt this paid call is frozen to. The
// authorization target itself is the tool's AuthorizationBinding; this layer
// never changes the action's identity or class.
type imageGenerationResolver struct {
	inner permissions.Resolver
	state func(sessionID string) *imageGenerationTurnState
}

// Resolve implements permissions.Resolver.
func (r imageGenerationResolver) Resolve(ctx context.Context, req permissions.ResolveRequest) (permissions.Action, error) {
	action, err := r.inner.Resolve(ctx, req)
	if err != nil || req.ToolName != imagegentool.Name || r.state == nil || req.Context == nil {
		return action, err
	}
	state := r.state(req.Context.SessionID)
	if state == nil {
		return action, nil
	}
	md := make(map[string]any, len(action.Metadata)+1)
	for k, v := range action.Metadata {
		md[k] = v
	}
	md[permissions.MetadataCommandSummary] = imageGenerationApprovalSummary(state.snapshot(), req.Input)
	action.Metadata = md
	return action, nil
}

func (a *App) imageGenerationStateOf(sessionID string) *imageGenerationTurnState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryLocked(sessionID); e != nil {
		return e.imageGeneration
	}
	if a.pendingImageGeneration != nil && a.pendingImageGeneration.sessionID == sessionID {
		return a.pendingImageGeneration
	}
	return nil
}

func imageGenerationApprovalSummary(snap imageGenerationSnapshot, input json.RawMessage) string {
	var in struct {
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
	}
	_ = json.Unmarshal(input, &in)
	size := strings.TrimSpace(in.Size)
	if size == "" {
		size = snap.Size
	}
	tenant := snap.Tenant
	if tenant == "" {
		tenant = "通行证默认租户"
	}
	return fmt.Sprintf("使用生图模型 %s（租户 %s）生成 1 张 %s 图片，会计费；提示词：%s", snap.Model, tenant, size, truncateRunes(strings.TrimSpace(in.Prompt), 300))
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
