package websearchtool

import (
	"strings"
	"testing"
)

func TestDescriptionExplainsPlatformNetworkBoundary(t *testing.T) {
	t.Parallel()
	tl := New(Config{
		Endpoint: "https://bridge.example.invalid/v1/websearch",
		Token:    func() (string, error) { return "unused-test-token", nil },
	})
	if tl == nil {
		t.Fatal("New returned nil for a complete config")
	}
	description := tl.Description()
	for name, fragments := range map[string][]string{
		"platform routing": {"requires approval", "upstream web search runs on the server", "RUNCODE_WEB_PROXY do not control", "local proxy cannot fix server-side"},
		"classification":   {"client-to-Bridge", "HTTP 401/403", "429", "5xx", "If the failing hop is unclear", "platform administrator"},
		"bounded checks":   {"bounded, read-only", "credentials redacted", "NO_PROXY mismatch", "do not change system settings"},
		"data boundary":    {"never copy Passport tokens into shell commands", "or route Bridge/OA requests", "Do not disable TLS verification", "disabled tools", "session data-access restrictions"},
		"honest feedback":  {"checks actually performed", "uncertainty", "actionable next step", "user's language", "only when the user permits it", "no private query/context", "not a successful platform search"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, fragment := range fragments {
				if !strings.Contains(description, fragment) {
					t.Errorf("missing platform troubleshooting guidance %q", fragment)
				}
			}
		})
	}
}
