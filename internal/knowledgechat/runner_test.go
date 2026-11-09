package knowledgechat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
)

// 可控事件流只替代网络；断流、长度与工具拒绝均经过生产消费逻辑。
type answerTestStream struct {
	events chan llm.StreamEvent
	err    error
	closed bool
}

func (s *answerTestStream) Events() <-chan llm.StreamEvent { return s.events }
func (s *answerTestStream) Err() error                     { return s.err }
func (s *answerTestStream) Close() error                   { s.closed = true; return nil }
func answerStream(events ...llm.StreamEvent) *answerTestStream {
	s := &answerTestStream{events: make(chan llm.StreamEvent, len(events))}
	for _, e := range events {
		s.events <- e
	}
	close(s.events)
	return s
}
func TestKnowledgeStreamFailure(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		stop       llm.StopReason
		streamErr  error
		wantErr    error
		truncated  bool
	}{
		{name: "success", text: "答案", stop: llm.StopReasonEndTurn},
		{name: "truncated", text: "部分", stop: llm.StopReasonMaxTokens, truncated: true},
		{name: "tool denied", stop: llm.StopReasonToolUse, wantErr: ErrToolUse},
		{name: "oversize thinking", text: strings.Repeat("x", 65537), stop: llm.StopReasonEndTurn, wantErr: ErrOutputLimit},
		{name: "broken after output", text: "部分", streamErr: &llm.Error{StatusCode: 401}, wantErr: ErrAnswer},
		{name: "missing stop", text: "部分", wantErr: ErrAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := answerStream(llm.StreamEvent{Type: llm.StreamEventTypeContentBlockDelta, Delta: &llm.ContentDelta{Thinking: tc.text}}, llm.StreamEvent{Type: llm.StreamEventTypeMessageStop, StopReason: tc.stop})
			stream.err = tc.streamErr
			calls, refresh := 0, 0
			result, err := Run(context.Background(), llm.Request{}, RunnerDependencies{
				Check:   func(context.Context) error { return nil },
				Open:    func(context.Context, llm.Request) (llm.Stream, error) { calls++; return stream, nil },
				Refresh: func(context.Context) error { refresh++; return nil },
			})
			if !errors.Is(err, tc.wantErr) || result.Truncated != tc.truncated || calls != 1 || refresh != 0 || !stream.closed {
				t.Fatalf("result=%+v err=%v calls=%d refresh=%d closed=%v", result, err, calls, refresh, stream.closed)
			}
		})
	}
}
func TestKnowledgeStreamRefreshOnce(t *testing.T) {
	calls, refresh, checks := 0, 0, 0
	_, err := Run(context.Background(), llm.Request{}, RunnerDependencies{
		Check: func(context.Context) error { checks++; return nil },
		Open: func(context.Context, llm.Request) (llm.Stream, error) {
			calls++
			return nil, &llm.Error{StatusCode: 401}
		},
		Refresh: func(context.Context) error { refresh++; return nil },
	})
	if !errors.Is(err, ErrAnswer) || calls != 2 || refresh != 1 || checks < 3 {
		t.Fatalf("err=%v calls=%d refresh=%d checks=%d", err, calls, refresh, checks)
	}
}
func TestKnowledgeCancelDuringStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &answerTestStream{events: make(chan llm.StreamEvent)}
	_, err := Run(ctx, llm.Request{}, RunnerDependencies{Check: func(context.Context) error { return nil }, Open: func(context.Context, llm.Request) (llm.Stream, error) { cancel(); return s, nil }})
	if !errors.Is(err, context.Canceled) || !s.closed {
		t.Fatal("取消未关闭流", err)
	}
}
