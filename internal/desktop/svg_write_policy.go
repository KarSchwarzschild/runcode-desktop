package desktop

// 生成 / 覆盖 .svg 时免弹「权限请求」。
//
// Write/Edit 在默认策略里一律 Ask（工作区内也要点一次）。模型画图时常连续写出
// simple.svg 一类文件，弹窗打断节奏且用户已在对话里要求生成。这里只把「目标全是
// .svg」的那次询问升级成放行；其它扩展名、Delete、Bash 重定向写文件一律不动。
//
// 不论路径是否在工作区内：资源上的越界标记仍在，executor 会照常置 OutsideAllowed，
// 工具边界守卫不会因为免弹窗而拒绝落盘。硬拒（read_stale / write_exists 等）原样透传。

import (
	"context"
	"path/filepath"
	"strings"

	"gitlab.ouc-online.com.cn/aibase/agentloop/permissions"
)

const reasonSVGWrite permissions.Reason = "svg_write"

type svgWritePolicy struct{ inner permissions.Policy }

func newSVGWritePolicy(inner permissions.Policy) svgWritePolicy {
	return svgWritePolicy{inner: inner}
}

func (p svgWritePolicy) Decide(ctx context.Context, action permissions.Action) permissions.Decision {
	decision := p.inner.Decide(ctx, action)
	if decision.FinalEffect != permissions.EffectAsk {
		return decision
	}
	if !isSVGFileMutation(action) {
		return decision
	}
	return permissions.Allow(reasonSVGWrite, "desktop.svg_write")
}

// isSVGFileMutation 要求：写/改操作，且每一个资源都是以 .svg 结尾的文件路径。
// 混有其它类型或命令行资源时不认，避免「顺带」放宽无关目标。
func isSVGFileMutation(action permissions.Action) bool {
	switch action.Operation {
	case permissions.OperationWrite, permissions.OperationEdit:
	default:
		return false
	}
	if len(action.Resources) == 0 {
		return false
	}
	for _, r := range action.Resources {
		if r.Type != permissions.ResourceFile {
			return false
		}
		if !isSVGPath(r.Path) {
			return false
		}
	}
	return true
}

func isSVGPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(path), ".svg")
}
