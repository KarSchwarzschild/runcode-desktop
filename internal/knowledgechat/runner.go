package knowledgechat

import (
	"context"
	"errors"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

var (
	// ErrAnswer 不透出上游正文，用于模型断流、协议错误或认证失败后的固定提示。
	ErrAnswer = errors.New("模型回答失败，请显式重试")
	// ErrToolUse 表示上游返回了知识库模式不允许执行的工具请求。
	ErrToolUse = errors.New("知识库问答不支持模型工具调用")
	// ErrOutputLimit 表示文字与 thinking 的合计 UTF-8 字节超过允许上限。
	ErrOutputLimit = errors.New("模型输出超过 64 KiB 限制，请缩小问题后重试")
)

// Answer 只保存用户可见结果；资料正文仍仅存在 Prepared.Request 内存中。
// Thinking 参与输出上限，但不作为后续问答上下文，避免扩大历史授权范围。
type Answer struct {
	Text, Thinking            string
	InputTokens, OutputTokens int
	Truncated                 bool
}

// RunnerDependencies 仅暴露公开 Provider.Stream 所需能力，不接入引擎会话或工具。
// Refresh 必须同时重验捕获的账号、服务边界和使用中的库/模型权限。
type RunnerDependencies struct {
	Check   func(context.Context) error
	Open    func(context.Context, llm.Request) (llm.Stream, error)
	Refresh func(context.Context) error
	Delta   func(text, thinking string)
}

// Run 在统一的 120 秒期限内消费单轮回答。自动重试仅限没有任何输出的
// 401，且至多刷新一次；已输出的回答失败后必须由用户发起新一轮重试。
func Run(ctx context.Context, req llm.Request, deps RunnerDependencies) (Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if deps.Check == nil || deps.Open == nil || len(req.Tools) != 0 {
		return Answer{}, ErrAnswer
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return Answer{}, err
		}
		if err := deps.Check(ctx); err != nil {
			return Answer{}, err
		}
		stream, err := deps.Open(ctx, req)
		result := Answer{}
		if err == nil {
			if stream == nil {
				return result, ErrAnswer
			}
			result, err = consumeAnswer(ctx, stream, deps)
			_ = stream.Close()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err == nil {
			return result, nil
		}
		providerErr, auth := llm.AsError(err)
		if attempt == 0 && auth && providerErr.StatusCode == 401 && result.Text == "" && result.Thinking == "" && deps.Refresh != nil {
			if err := deps.Check(ctx); err != nil {
				return result, err
			}
			if err := deps.Refresh(ctx); err != nil {
				return result, err
			}
			continue
		}
		// 不把上游错误 body/URL 带入日志、文件或界面，其中可能包含资料或令牌。
		if errors.Is(err, ErrToolUse) || errors.Is(err, ErrOutputLimit) {
			return result, err
		}
		return result, ErrAnswer
	}
	return Answer{}, ErrAnswer
}

func consumeAnswer(ctx context.Context, stream llm.Stream, deps RunnerDependencies) (Answer, error) {
	result := Answer{}
	stopped := false
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case event, ok := <-stream.Events():
			if !ok {
				if err := stream.Err(); err != nil {
					return result, err
				}
				if !stopped {
					return result, ErrAnswer
				}
				return result, nil
			}
			if err := deps.Check(ctx); err != nil {
				return result, err
			}
			if event.StopReason == llm.StopReasonToolUse || event.Block != nil && event.Block.Type == llm.ContentBlockTypeToolUse || event.Delta != nil && len(event.Delta.InputJSON) > 0 {
				return result, ErrToolUse
			}
			text, thinking := "", ""
			if event.Type == llm.StreamEventTypeContentBlockStart && event.Block != nil {
				switch event.Block.Type {
				case llm.ContentBlockTypeThinking:
					thinking = event.Block.Text
				case llm.ContentBlockTypeText:
					text = event.Block.Text
				}
			}
			if event.Type == llm.StreamEventTypeContentBlockDelta && event.Delta != nil {
				text, thinking = event.Delta.Text, event.Delta.Thinking
			}
			if stopped && (text != "" || thinking != "") {
				return result, ErrAnswer
			}
			// 上限用 UTF-8 字节计数，同时约束文字和 thinking；越界片段不对外发送。
			if len(result.Text)+len(result.Thinking)+len(text)+len(thinking) > 64<<10 {
				return result, ErrOutputLimit
			}
			result.Text += text
			result.Thinking += thinking
			if (text != "" || thinking != "") && deps.Delta != nil {
				deps.Delta(text, thinking)
			}
			if event.Usage != nil {
				result.InputTokens = max(result.InputTokens, event.Usage.InputTokens)
				result.OutputTokens = max(result.OutputTokens, event.Usage.OutputTokens)
			}
			if event.Type == llm.StreamEventTypeMessageStop {
				switch event.StopReason {
				case llm.StopReasonEndTurn, llm.StopReasonStopSequence:
					stopped = true
				case llm.StopReasonMaxTokens:
					stopped = true
					result.Truncated = true
				default:
					return result, ErrAnswer
				}
			}
		}
	}
}
