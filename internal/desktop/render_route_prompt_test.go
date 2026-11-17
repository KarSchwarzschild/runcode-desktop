package desktop

import (
	"strings"
	"testing"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
)

func TestRenderRoutePromptMergesIntoSessionConfig(t *testing.T) {
	app := New(&recordingSink{})
	ws := t.TempDir()
	cfg := engine.Config{
		CWD:                ws,
		PermissionMode:     "interactive",
		SystemPromptAppend: "caller-sentinel",
	}
	sctx := host.SessionContext{
		ID:       "render-route",
		Approver: host.NewAsyncApprover(func(string, any) {}, ws),
		Emit:     func(string, any) {},
	}
	var opts engine.Options
	app.configureSession(sctx, &cfg, &opts)

	// 通过真实会话配置 hook 检查合并结果，避免只测试常量却漏掉接线。
	for _, want := range []string{
		"caller-sentinel",
		localBrowserPrompt,
		renderRoutePrompt,
		"Mermaid",
		"SVG",
		"HTML",
		"image_generate",
		"Do not create an HTML file only to preview Mermaid",
		"Do not use image generation for ordinary flowcharts",
	} {
		if !strings.Contains(cfg.SystemPromptAppend, want) {
			t.Errorf("SystemPromptAppend missing %q", want)
		}
	}
}
