// Package previewtool implements the open_preview tool: the model asks the desktop
// to open an authorized local file in its preview panel. It validates the concrete
// per-call read scope and emits a source-bound reference via the desktop opener. It is
// registered only in the desktop (via engine.Options.ExtraTools), never the CLI.
//
// 名字带 tool 后缀是为了与 internal/desktop 自己的 preview.go(工作区静态预览
// 服务器)区分:那是"预览服务",这里是"给模型用的预览工具"。
package previewtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.ouc-online.com.cn/aibase/agentloop/tool"
	"gitlab.ouc-online.com.cn/aibase/agentloop/toolpath"
)

// previewData is the structured payload the desktop UI reads off the event to open
// a preview tab. In-process UI only.
type previewData struct {
	Path string `json:"path"`
}

type input struct {
	Path string `json:"path"`
}

// Name is the wire name the model calls. Exported so the desktop can key this
// tool's permission classification off it (engine.Options.ToolClasses and the
// session's resolver) without repeating the string.
const Name = "open_preview"

// Opener registers an actually authorized preview in its owning session.
type Opener func(context.Context, string, *tool.Context) (any, error)

// Tool is the open_preview tool.
type Tool struct{ open Opener }

// New returns the open_preview tool.
func New(open ...Opener) tool.Tool {
	var t Tool
	if len(open) > 0 {
		t.open = open[0]
	}
	return t
}

// Name is the tool name the model calls.
func (Tool) Name() string { return Name }

// InputPresentation previews the primary argument while the model composes it.
func (Tool) InputPresentation() tool.InputPresentation {
	return tool.InputPresentation{PrimaryField: "path"}
}

// Description tells the model when to reach for this tool.
func (Tool) Description() string {
	return "Open an authorized local file in the user's desktop preview panel. Call this after you " +
		"produce a document or a website/H5 (e.g. an .html, .md, or image) so the user sees it " +
		"immediately. Use a workspace-relative path or an authorized absolute path."
}

// InputSchema declares a workspace-relative or authorized absolute file path.
func (Tool) InputSchema() tool.Schema {
	return tool.Schema{
		Type: tool.SchemaTypeObject,
		Properties: map[string]tool.Schema{
			"path": {Type: tool.SchemaTypeString, Description: "Workspace-relative or authorized absolute file path."},
		},
		Required:             []string{"path"},
		AdditionalProperties: false,
	}
}

// IsConcurrencySafe reports true: opening a preview only reads and emits an event.
func (Tool) IsConcurrencySafe() bool { return true }

// Run validates the concrete read target and emits the desktop opener result.
func (t Tool) Run(ctx context.Context, raw json.RawMessage, tctx *tool.Context, out chan<- tool.Event) (tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return tool.Result{}, fmt.Errorf("parse open_preview input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return tool.Result{}, errors.New("path is required")
	}
	ws, err := toolpath.WorkspaceRoot(tctx)
	if err != nil {
		return tool.Result{}, err
	}
	abs, err := toolpath.Resolve(in.Path, tctx)
	if err != nil {
		return tool.Result{}, err
	}
	if err := toolpath.RequireReadScope(abs, tctx); err != nil {
		return tool.Result{}, fmt.Errorf("path is outside the workspace: %s", in.Path)
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return tool.Result{}, fmt.Errorf("file not found: %s", in.Path)
	}
	rel, err := filepath.Rel(ws, abs)
	if err != nil {
		rel = in.Path
	}
	if within, _ := toolpath.IsWithinResolved(ws, abs); !within {
		rel = abs
	}
	rel = filepath.ToSlash(rel)
	var data any = previewData{Path: rel}
	if t.open != nil {
		data, err = t.open(ctx, abs, tctx)
		if err != nil {
			return tool.Result{}, err
		}
	}
	if out != nil {
		select {
		case out <- tool.Event{Type: tool.EventTypeProgress, ToolName: "open_preview", Message: "预览 " + rel, Data: data}:
		default:
		}
	}
	return tool.Result{Content: []tool.ResultContent{{Type: tool.ResultContentTypeText, Text: "已在桌面打开预览：" + rel}}}, nil
}
