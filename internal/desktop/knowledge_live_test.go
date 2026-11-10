package desktop

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Normal browser login coverage is retained independently of the retired
// engine question-context integration. Host QA live checks belong to Task 7.
func stage4LiveConfiguration(t *testing.T) {
	t.Helper()
	pc := passportConfig()
	if pc.Authority != "https://passport-ai-dev.ouc-online.com.cn" || pc.BridgeBaseURL != "http://127.0.0.1:18199" || pc.KnowledgeBaseURL != pc.BridgeBaseURL {
		t.Fatal("Stage 4 live checks require the dev Passport and local Bridge for both model and knowledge requests")
	}
	profile := os.Getenv("RUNCODE_STAGE4_PROFILE")
	actual, err := os.UserConfigDir()
	if err != nil || profile == "" || filepath.Clean(profile) != filepath.Clean(actual) {
		t.Fatal("configure the dedicated Stage 4 test profile before live checks")
	}
	if _, err := os.Stat(filepath.Join(profile, "stage4-test-profile.marker")); err != nil {
		t.Fatal("dedicated Stage 4 test profile marker is missing")
	}
}

func stage4CheckLoginIssuer(t *testing.T, app *App) {
	t.Helper()
	app.tokens.mu.Lock()
	token := app.tokens.ts.AccessToken
	app.tokens.mu.Unlock()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("a normal dev Passport login is required")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Issuer string `json:"iss"`
		UserID string `json:"UserId"`
	}
	if err != nil || json.Unmarshal(body, &claims) != nil || claims.Issuer != passportConfig().Authority || claims.UserID == "" {
		t.Fatal("saved login does not belong to the configured dev Passport; log in normally again")
	}
	// This is a local destination guard only. The Bridge verifies JWT signature,
	// issuer, expiry and scope before trusting UserId for authorization.
}

func TestStage4PassportLogin(t *testing.T) {
	if os.Getenv("RUNCODE_STAGE4_OAUTH_LOGIN") != "1" {
		t.Skip("opt in to the normal browser login in the dedicated test profile")
	}
	stage4LiveConfiguration(t)
	app := New(&recordingSink{})
	defer func() { _ = app.CloseAllSessions() }()
	status, err := app.PassportLogin("")
	if err != nil || !status.LoggedIn {
		t.Fatalf("normal test Passport login did not complete (%T)", err)
	}
	stage4CheckLoginIssuer(t, app)
	t.Log("normal dev Passport login saved in the encrypted dedicated test profile")
}
