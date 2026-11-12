package desktop

import (
	"context"
	"sync"

	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

// scriptedProvider drives deterministic ReAct rounds for knowledge_search tool tests.
type scriptedProvider struct {
	mu       sync.Mutex
	requests []llm.Request
	steps    []scriptedStep
	idx      int
}

type scriptedStep struct {
	kind  string // text | tool
	text  string
	name  string
	id    string
	input string
}

func (p *scriptedProvider) Name() string { return "desktop-knowledge-coexistence" }
func (p *scriptedProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}
func (p *scriptedProvider) Stream(_ context.Context, req llm.Request) (llm.Stream, error) {
	p.mu.Lock()
	p.requests = append(p.requests, cloneLLMRequest(req))
	i := p.idx
	if i >= len(p.steps) {
		i = len(p.steps) - 1
	}
	step := p.steps[i]
	p.idx++
	p.mu.Unlock()
	ch := make(chan llm.StreamEvent, 8)
	switch step.kind {
	case "tool":
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStart, Block: &llm.ContentBlock{Type: llm.ContentBlockTypeToolUse, ID: step.id, Name: step.name}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockDelta, Delta: &llm.ContentDelta{InputJSON: []byte(step.input)}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeMessageStop, StopReason: llm.StopReasonToolUse, Usage: &llm.Usage{OutputTokens: 1}}
	default:
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStart, Block: &llm.ContentBlock{Type: llm.ContentBlockTypeText, Text: step.text}}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeMessageStop, StopReason: llm.StopReasonEndTurn, Usage: &llm.Usage{OutputTokens: 2}}
	}
	close(ch)
	return scriptedStream{ch}, nil
}

type scriptedStream struct{ ch <-chan llm.StreamEvent }

func (s scriptedStream) Events() <-chan llm.StreamEvent { return s.ch }
func (scriptedStream) Err() error                       { return nil }
func (scriptedStream) Close() error                     { return nil }

func cloneLLMRequest(req llm.Request) llm.Request {
	out := req
	if req.Tools != nil {
		out.Tools = append([]llm.ToolSpec(nil), req.Tools...)
	}
	if req.Messages != nil {
		out.Messages = append([]llm.Message(nil), req.Messages...)
	}
	if req.System != nil {
		out.System = append([]llm.ContentBlock(nil), req.System...)
	}
	return out
}

var coexistenceProviderOnce sync.Once
var coexistenceActive = &scriptedProvider{}

func resetScriptedProvider(p *scriptedProvider, steps []scriptedStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = nil
	p.steps = steps
	p.idx = 0
}
