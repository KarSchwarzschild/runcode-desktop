package knowledgechat

import (
	"context"
	"time"
)

// Dependencies 由桌面绑定已经捕获的账号和服务地址。此接口刻意不暴露
// engine.Session 或工具能力，使检索准备无法越过纯文字问答边界。
type Dependencies struct {
	Check            func(context.Context, Scope) error
	Retrieve         func(context.Context, Scope, []string, string) (Retrieved, error)
	AuthorizeLibrary func(context.Context, Scope, string) error
	AuthorizeModel   func(context.Context, Scope, string) error
}

// Prepare 检索本轮资料，并在裁剪后校验实际保留历史的库权限和模型权限。
func Prepare(ctx context.Context, scope Scope, cfg RequestConfig, q Question, history []Question, deps Dependencies) (Prepared, error) {
	// 多库检索、历史依赖鉴权及模型授权共享 30 秒，不能逐项重置超时。
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if deps.Check == nil || deps.Retrieve == nil || deps.AuthorizeLibrary == nil || deps.AuthorizeModel == nil {
		return Prepared{}, ErrProtocol
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return deps.Check(ctx, scope)
	}
	if err := check(); err != nil {
		return Prepared{}, err
	}
	r := Retrieved{Status: "empty", Libraries: []Library{}, Hits: []Hit{}}
	if len(q.Selection.LibraryIDs) > 0 {
		var err error
		r, err = deps.Retrieve(ctx, scope, append([]string{}, q.Selection.LibraryIDs...), q.Text)
		if err != nil {
			return Prepared{}, err
		}
		if err := check(); err != nil {
			return Prepared{}, err
		}
	}
	p, err := BuildRequest(cfg, q, history, r)
	if err != nil {
		return Prepared{}, err
	}
	authorized := map[string]bool{}
	// 本轮选库已经由 retrieve 整体授权；只补查裁剪后真正保留的历史依赖，
	// 去重后逐库检查，避免目录分页时的无界详情请求放大。
	for _, id := range q.Selection.LibraryIDs {
		authorized[id] = true
	}
	for _, id := range p.AffectedLibraryIDs {
		if authorized[id] {
			continue
		}
		if err := check(); err != nil {
			return Prepared{}, err
		}
		if err := deps.AuthorizeLibrary(ctx, scope, id); err != nil {
			return Prepared{}, err
		}
		authorized[id] = true
		if err := check(); err != nil {
			return Prepared{}, err
		}
	}
	if err := deps.AuthorizeModel(ctx, scope, cfg.Model); err != nil {
		return Prepared{}, err
	}
	if err := check(); err != nil {
		return Prepared{}, err
	}
	return p, nil
}
