// Package localusage 提供本机 SQLite 用量记录与平台 LLM Provider 采集包装。
package localusage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

// 桌面辅助 LLM（自动标题／安全裁判／压缩摘要／授权评估）会再走一次 Provider.Stream，
// 但不该算作用户可见的「一次请求」。引擎把 purpose 只写进遥测，不改 Core 时只能
// 用请求正文里的稳定锚点识别；匹配到的调用跳过 Begin／Finish。
var auxiliaryLLMMarkers = []string{
	"Title this request:",                                                 // session_title 用户句
	"You name a coding-assistant conversation.",                           // session_title 系统句
	"You are condensing a coding-assistant conversation to save context.", // compaction
	"Conversation to summarize:",                                          // compaction 用户句
	"You are a safety gate for a coding agent.",                           // harm_check
	"You are an authorization gate, not the executing agent.",             // action_assessment
}

// PlatformProviderName 是桌面注册的固定平台适配器名；用户设置里仍写 openai／passport 语义，
// 不把此内部名写入模型列表。
const PlatformProviderName = "desktop-platform-openai"

// 调用计数定义：一次对底层 Provider.Stream 的发起计为一次客户端模型调用；
// SDK 内部不可见的 HTTP 重试不拆分；外层重新 Stream 为新调用。

// WrapPlatformProvider 包装底层 provider：仅平台路径应调用。已是本包装则原样返回（幂等）。
// freeze 在 Stream 开始时取冻结 Scope；失败则跳过记账且不改变业务错误。
// 默认 callKind=llm（主聊天／标题／压缩／子代理等）。
func WrapPlatformProvider(base llm.Provider, freeze func(context.Context) (Scope, error), recorder Recorder) llm.Provider {
	return WrapPlatformProviderKind(base, freeze, recorder, CallKindLLM)
}

// WrapPlatformProviderKind 同 WrapPlatformProvider，可指定 callKind（如 vision）。
func WrapPlatformProviderKind(base llm.Provider, freeze func(context.Context) (Scope, error), recorder Recorder, kind string) llm.Provider {
	if base == nil {
		return nil
	}
	if _, ok := base.(*platformProvider); ok {
		return base
	}
	if recorder == nil || freeze == nil {
		return base
	}
	if kind == "" {
		kind = CallKindLLM
	}
	return &platformProvider{base: base, freeze: freeze, rec: recorder, kind: kind}
}

type platformProvider struct {
	base   llm.Provider
	freeze func(context.Context) (Scope, error)
	rec    Recorder
	kind   string
}

func (p *platformProvider) Name() string { return p.base.Name() }

func (p *platformProvider) Capabilities() llm.Capabilities { return p.base.Capabilities() }

func (p *platformProvider) Stream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	if isAuxiliaryLLMRequest(req) {
		return p.base.Stream(ctx, req)
	}
	scope, scopeErr := p.freeze(ctx)
	callID := newCallID()
	started := time.Now()
	monoStart := time.Now()
	var began bool
	kind := p.kind
	if kind == "" {
		kind = CallKindLLM
	}
	if scopeErr == nil && scope.ScopeID != "" {
		start := StartRecord{
			CallID:    callID,
			Scope:     scope,
			Model:     req.Model,
			CallKind:  kind,
			StartedAt: started.UTC(),
		}
		// 统计写入失败不阻断业务。
		if err := p.rec.Begin(ctx, start); err == nil {
			began = true
		}
	}
	inner, err := p.base.Stream(ctx, req)
	if err != nil {
		if began {
			_ = p.rec.Finish(context.Background(), FinishRecord{
				CallID: callID, Status: StatusFailed, FinishedAt: time.Now().UTC(),
				ErrorCode: "stream_open_failed", FrozenStart: StartRecord{CallID: callID, Scope: scope, Model: req.Model, CallKind: kind, StartedAt: started.UTC()},
			})
		}
		return nil, err
	}
	if !began {
		return inner, nil
	}
	return &countingStream{
		inner:     inner,
		rec:       p.rec,
		callID:    callID,
		scope:     scope,
		model:     req.Model,
		kind:      kind,
		started:   started.UTC(),
		monoStart: monoStart,
		done:      make(chan struct{}),
	}, nil
}

type countingStream struct {
	inner     llm.Stream
	rec       Recorder
	callID    string
	scope     Scope
	model     string
	kind      string
	started   time.Time
	monoStart time.Time

	mu        sync.Mutex
	lastUsage *llm.Usage
	usageSeen bool
	finished  atomic.Bool
	done      chan struct{}
	out       chan llm.StreamEvent
	once      sync.Once
}

func (s *countingStream) Events() <-chan llm.StreamEvent {
	s.once.Do(func() {
		s.out = make(chan llm.StreamEvent)
		go s.pump()
	})
	return s.out
}

func (s *countingStream) pump() {
	defer close(s.out)
	events := s.inner.Events()
	for {
		var ev llm.StreamEvent
		var ok bool
		select {
		case <-s.done:
			return
		case ev, ok = <-events:
			if !ok {
				status, code := streamEndStatus(s.inner.Err())
				s.finish(status, code)
				return
			}
		}
		if ev.Usage != nil {
			s.mu.Lock()
			cp := *ev.Usage
			s.lastUsage = &cp
			s.usageSeen = true
			s.mu.Unlock()
		}
		select {
		case s.out <- ev:
		case <-s.done:
			return
		}
	}
}

func (s *countingStream) Err() error { return s.inner.Err() }

func (s *countingStream) Close() error {
	err := s.inner.Close()
	streamErr := s.inner.Err()
	if streamErr == nil {
		streamErr = err
	}
	status, code := streamEndStatus(streamErr)
	s.finish(status, code)
	return err
}

func streamEndStatus(err error) (string, string) {
	if errors.Is(err, context.Canceled) {
		return StatusCancelled, "canceled"
	}
	if err != nil {
		return StatusUnknown, "stream_err"
	}
	return StatusSucceeded, ""
}

// 终态竞争：Close / 泵结束 / 取消只能落一次 Finish。
func (s *countingStream) finish(status, code string) {
	if !s.finished.CompareAndSwap(false, true) {
		return
	}
	if s.done != nil {
		close(s.done)
	}
	s.mu.Lock()
	usage := s.lastUsage
	seen := s.usageSeen
	s.mu.Unlock()
	dur := time.Since(s.monoStart).Milliseconds()
	fin := FinishRecord{
		CallID: s.callID, Status: status, FinishedAt: time.Now().UTC(),
		DurationMs: &dur, ErrorCode: code, TokenSource: "unavailable",
		FrozenStart: StartRecord{
			CallID: s.callID, Scope: s.scope, Model: s.model, CallKind: s.kind, StartedAt: s.started,
		},
	}
	if seen && usage != nil {
		fin.TokenSource = "reported"
		// 无 presence 字段：正值视为已知；0 保持未知（引擎缺省压 0）。
		if usage.InputTokens > 0 {
			v := int64(usage.InputTokens)
			fin.InputTokens, fin.InputTokensKnown = &v, true
		}
		if usage.OutputTokens > 0 {
			v := int64(usage.OutputTokens)
			fin.OutputTokens, fin.OutputTokensKnown = &v, true
		}
		// AI.core 将缓存写入视为未命中、缓存读取视为命中；两者都为 0
		// 时保持未知，避免把供应商未上报缓存误显示成命中率 0%。
		if usage.CacheReadInputTokens > 0 {
			v := int64(usage.CacheReadInputTokens)
			fin.CacheHitTokens, fin.CacheHitTokensKnown = &v, true
		}
		if usage.CacheCreationInputTokens > 0 {
			v := int64(usage.CacheCreationInputTokens)
			fin.CacheMissTokens, fin.CacheMissTokensKnown = &v, true
		}
		// 明确 known-zero：测试可通过 Metadata 注入；生产 OpenAI 路径无 presence 时不把 0 当已知。
	}
	_ = s.rec.Finish(context.Background(), fin)
}

// isAuxiliaryLLMRequest 识别引擎侧辅助调用：无工具、正文命中桌面已知锚点。
// 有 Tools 的主回合／子代理请求一律不当辅助，避免误伤。
func isAuxiliaryLLMRequest(req llm.Request) bool {
	if len(req.Tools) > 0 {
		return false
	}
	var b strings.Builder
	for _, block := range req.System {
		if block.Type == llm.ContentBlockTypeText {
			b.WriteString(block.Text)
			b.WriteByte('\n')
		}
	}
	for _, msg := range req.Messages {
		b.WriteString(llm.TextContent(msg))
		b.WriteByte('\n')
	}
	body := b.String()
	if body == "" {
		return false
	}
	for _, marker := range auxiliaryLLMMarkers {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// NewCallID 生成全局唯一调用 ID（生图应改用既有 generationID，勿用本函数）。
func NewCallID() string { return newCallID() }

func newCallID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}
