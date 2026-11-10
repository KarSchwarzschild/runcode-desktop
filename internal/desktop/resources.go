package desktop

// 技能(skills)与子代理(agents)是同构的"可管理资源":都放在两个作用域下的同名
// 目录里——
//
//	项目级  <workspace>/.runcode/<kind>
//	用户级  <os.UserConfigDir>/runcode/<kind>
//
// 目录解析、命名规则与 frontmatter 处理只在这里定义一次,skills.go 与 agents.go
// 共用;两边原先各写一份、改一处就要记得改另一处。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gitlab.ouc-online.com.cn/aibase/agentloop/frontmatter"
)

// resourceKind is the directory name a resource lives under in either scope.
type resourceKind string

const (
	kindSkills resourceKind = "skills"
	kindAgents resourceKind = "agents"
)

// projectResourceDir is the workspace directory holding a kind's project-level
// resources; "" when there is no active session (and therefore no workspace).
func (a *App) projectResourceDir(kind resourceKind) string {
	a.mu.Lock()
	ws := a.workspace
	a.mu.Unlock()
	if ws == "" {
		return ""
	}
	return filepath.Join(ws, ".runcode", string(kind))
}

// userResourceDir is the per-user (global) directory for a kind; "" when the OS
// config directory cannot be located.
func userResourceDir(kind resourceKind) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "runcode", string(kind))
}

// resourceRoot resolves the directory for a scope ("project" or "user"). Any
// value other than "user" is treated as project scope, matching the frontend's
// two-way scope switch.
func (a *App) resourceRoot(kind resourceKind, scope string) (string, error) {
	switch scope {
	case "user":
		root := userResourceDir(kind)
		if root == "" {
			return "", errors.New("无法定位用户配置目录")
		}
		return root, nil
	default: // project
		root := a.projectResourceDir(kind)
		if root == "" {
			return "", errNoSession
		}
		return root, nil
	}
}

// validResourceName mirrors the skill package's name rule. Sub-agents share it so
// a name is valid in exactly one way across both kinds — and so neither can write
// a file outside its root (no separators, no dots).
func validResourceName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// collapseLine normalizes intentionally single-line fields, such as display
// names and tool lists. YAML quoting, not flattening, makes arbitrary text safe.
func collapseLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

// frontmatterName extracts the name from a "--- ... ---" frontmatter block, as
// used by both SKILL.md and a sub-agent's .md.
func frontmatterName(content string) string {
	return frontmatterValue(content, "name")
}

// frontmatterValue returns the first of keys present in the "--- ... ---" block
// (case-insensitive). Several spellings are accepted per call because the same
// datum arrives under different conventions — the platform's API says
// display_name, a hand-written SKILL.md follows the allowed-tools style and says
// display-name — and neither is worth rejecting a file over.
//
// Syntax is shared with the engine so import names and display metadata agree
// with the catalog consumed by the model.
func frontmatterValue(content string, keys ...string) string {
	doc, err := frontmatter.Parse(content)
	if err != nil {
		return ""
	}
	value, err := doc.Scalar(keys...)
	if err != nil {
		return ""
	}
	return value
}
