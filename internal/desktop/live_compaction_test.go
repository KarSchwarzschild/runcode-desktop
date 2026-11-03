package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/llm"
	"gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// preserveDesktopConfig snapshots the real desktop.json and restores it after the
// test. Live tests drive App.StartSession, which persists its request via
// saveConfig — without this guard a test's throwaway budget/workspace would
// clobber the user's actual configuration (the exact 200k→2k regression this
// prevents). Restores the original bytes, or removes the file if none existed.
func preserveDesktopConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(os.Getenv("APPDATA"), "runcode", "desktop.json")
	orig, err := os.ReadFile(path)
	t.Cleanup(func() {
		if err != nil {
			_ = os.Remove(path)
			return
		}
		_ = os.WriteFile(path, orig, 0o600)
	})
}

// Reopening resolves the saved profile's budget, not the abandoned global value.
func TestReopenCarriesConfiguredContextBudget(t *testing.T) {
	isolateConfigDir(t)
	app := New(&recordingSink{})
	saveContextFixture(t, app, "reopen", "m", "https://example.invalid/v1", ModelContextValues{MaxOutputTokens: 1024, MaxContextTokens: 18000})
	info, err := app.StartSession(StartSessionRequest{CWD: t.TempDir(), CustomModelName: "reopen", Model: "m", Provider: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseSession(info.SessionID)
	if info.MaxContextTokens != 18000 {
		t.Fatalf("budget = %d", info.MaxContextTokens)
	}
}

// waitForTurns blocks until the sink has recorded at least n EventTurnEnd events
// (or an EventTurnError), returning the collected TurnEnd payloads in order.
// Session events arrive envelope-wrapped (the host's emitter), so the payload
// sits inside the protocol.Envelope.
func waitForTurns(t *testing.T, sink *recordingSink, n int, deadline time.Time) []TurnEnd {
	t.Helper()
	for {
		var ends []TurnEnd
		sink.mu.Lock()
		for _, ev := range sink.events {
			env, ok := ev.data.(protocol.Envelope)
			if !ok {
				continue
			}
			switch ev.name {
			case EventTurnEnd:
				if te, ok := env.Payload.(TurnEnd); ok {
					ends = append(ends, te)
				}
			case EventTurnError:
				sink.mu.Unlock()
				t.Fatalf("turn errored: %+v", env.Payload)
			}
		}
		sink.mu.Unlock()
		if len(ends) >= n {
			return ends
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d turns (have %d)", n, len(ends))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestLiveDesktopAutoCompaction drives several real turns through the App against
// the configured endpoint with a deliberately tiny context budget, so automatic
// compaction fires. It asserts the usage-bar data (TurnEnd.ContextTokens) is
// populated and that the working history ends up carrying a summary message —
// proving both the usage bar and auto-compaction end to end. Skipped unless
// RUNCODE_LIVE=1.
func TestLiveDesktopAutoCompaction(t *testing.T) {
	if os.Getenv("RUNCODE_LIVE") != "1" {
		t.Skip("set RUNCODE_LIVE=1 to run the live desktop compaction probe")
	}
	preserveDesktopConfig(t)
	raw, err := os.ReadFile(filepath.Join(os.Getenv("APPDATA"), "runcode", "desktop.json"))
	if err != nil {
		t.Fatalf("read desktop.json: %v", err)
	}
	var stored desktopConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("parse desktop.json: %v", err)
	}

	saved := stored.startRequest()
	sink := &recordingSink{}
	app := New(sink)
	saveContextFixture(t, app, "live-compaction-probe", saved.Model, saved.BaseURL, ModelContextValues{MaxOutputTokens: 1024, MaxContextTokens: 2000})
	// A tiny budget forces compaction: real input tokens (system prompt + history)
	// dwarf 2000, so every turn is over the 0.8 threshold; once >4 turns exist the
	// oldest are summarized.
	_, err = app.StartSession(StartSessionRequest{
		CWD:             t.TempDir(),
		Provider:        saved.Provider,
		Model:           saved.Model,
		BaseURL:         saved.BaseURL,
		APIKey:          saved.APIKey,
		AuthToken:       saved.AuthToken,
		PermissionMode:  "flight",
		CustomModelName: "live-compaction-probe",
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer app.CloseSession("")

	const turns = 7
	prompts := []string{
		"用一句话说明数字 1 的两倍是多少。", "用一句话说明数字 2 的两倍是多少。",
		"用一句话说明数字 3 的两倍是多少。", "用一句话说明数字 4 的两倍是多少。",
		"用一句话说明数字 5 的两倍是多少。", "用一句话说明数字 6 的两倍是多少。",
		"用一句话说明数字 7 的两倍是多少。",
	}
	deadline := time.Now().Add(240 * time.Second)
	for i := 0; i < turns; i++ {
		if err := app.SendMessage("", prompts[i]); err != nil {
			t.Fatalf("SendMessage %d: %v", i, err)
		}
		waitForTurns(t, sink, i+1, deadline) // serialize: one turn at a time
	}

	ends := waitForTurns(t, sink, turns, deadline)
	// Usage bar: every turn should report a positive context occupancy.
	populated := 0
	for _, e := range ends {
		if e.ContextTokens > 0 {
			populated++
		}
	}
	t.Logf("contextTokens per turn: %v", contextTokensOf(ends))
	if populated == 0 {
		t.Fatal("no turn reported ContextTokens > 0 — usage bar would stay empty")
	}

	// Auto-compaction: the working history should now begin with / contain a summary
	// message condensing the earliest turns.
	session, err := app.engineSession()
	if err != nil {
		t.Fatalf("engineSession: %v", err)
	}
	history := session.History()
	if !hasSummaryMessage(history) {
		t.Fatalf("expected a compaction summary in history after %d turns, found none (len=%d)", turns, len(history))
	}
	t.Logf("history length after %d turns = %d messages (compacted, summary present)", turns, len(history))
}

func contextTokensOf(ends []TurnEnd) []int {
	out := make([]int, len(ends))
	for i, e := range ends {
		out[i] = e.ContextTokens
	}
	return out
}

func hasSummaryMessage(history []llm.Message) bool {
	for _, m := range history {
		for _, b := range m.Content {
			if b.Type == llm.ContentBlockTypeText && strings.Contains(b.Text, "Summary of earlier conversation") {
				return true
			}
		}
	}
	return false
}
