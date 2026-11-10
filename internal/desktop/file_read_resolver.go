package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wt68/runcode/internal/officetool"
	"github.com/wt68/runcode/internal/previewtool"
	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
)

type fileReadResolver struct{ inner permissions.Resolver }

func (r fileReadResolver) Resolve(ctx context.Context, req permissions.ResolveRequest) (permissions.Action, error) {
	if req.ToolName != officetool.Name && req.ToolName != previewtool.Name {
		return r.inner.Resolve(ctx, req)
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Input, &in); err != nil || strings.TrimSpace(in.Path) == "" {
		return permissions.Action{ToolName: req.ToolName}, fmt.Errorf("%w: file path required", permissions.ErrInvalidInput)
	}
	return permissions.ResolveReadPaths(req.ToolName, []string{in.Path}, req.Context)
}
