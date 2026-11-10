package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A test knowledge gateway must not move the registered login callback or the
// platform model destination, and the user's token must reach only that gateway.
func TestKnowledgeGatewayOverrideKeepsPassportAndModelRouting(t *testing.T) {
	knowledge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/t/tenant-A/v1/knowledge-bases" || r.Header.Get("Authorization") != "Bearer "+knowledgeJWT("A") {
			t.Errorf("wrong knowledge request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[],"page":1,"pageSize":10,"total":0,"hasMore":false}`))
	}))
	defer knowledge.Close()
	t.Setenv("RUNCODE_KNOWLEDGE_BRIDGE_BASE_URL", knowledge.URL+"/")
	app, id := realKnowledgeApp(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("knowledge request reached the platform model gateway")
		w.WriteHeader(http.StatusNotFound)
	})
	page, err := app.ListKnowledgeBases(KnowledgeListRequest{SessionID: id})
	if err != nil || page.Mode != "real" {
		t.Fatalf("separate knowledge gateway unavailable: %+v %v", page, err)
	}
	pc := passportConfig()
	if pc.RedirectURI != pc.BridgeBaseURL+"/oauth/callback" || strings.HasPrefix(pc.RedirectURI, knowledge.URL) {
		t.Fatalf("knowledge override moved the registered callback: %s", pc.RedirectURI)
	}
	cfg, err := buildConfig(StartSessionRequest{CWD: t.TempDir(), Provider: "passport", Model: "platform-model"})
	if err != nil {
		t.Fatal(err)
	}
	cfg = app.applyPassport(cfg, StartSessionRequest{Provider: "passport", TenantID: "tenant-A"})
	if cfg.BaseURL != pc.BridgeBaseURL+"/t/tenant-A/v1" {
		t.Fatalf("knowledge override moved model destination: %s", cfg.BaseURL)
	}
}

// A local API gateway can use an already registered OAuth relay. The relay
// controls only the login callback; tenant/model requests stay on the local API.
func TestPassportLocalBridgeUsesExplicitRegisteredCallback(t *testing.T) {
	t.Setenv("RUNCODE_PASSPORT_AUTHORITY", "https://passport-ai-dev.ouc-online.com.cn")
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", "http://127.0.0.1:18199")
	t.Setenv("RUNCODE_PASSPORT_REDIRECT_URI", "https://bridge-aibase.ouc-online.com.cn/oauth/callback")
	pc := passportConfig()
	if pc.RedirectURI != "https://bridge-aibase.ouc-online.com.cn/oauth/callback" {
		t.Fatalf("registered callback replaced by local API address: %s", pc.RedirectURI)
	}
	if pc.BridgeBaseURL != "http://127.0.0.1:18199" || pc.tokenURL() != "https://passport-ai-dev.ouc-online.com.cn/connect/token" {
		t.Fatal("callback override changed API or token destination")
	}
}

func TestKnowledgeDirectoryUsesConfiguredPortal(t *testing.T) {
	t.Setenv("RUNCODE_KNOWLEDGE_PORTAL_BASE_URL", "https://aibase-preview.ouc-online.com.cn/")
	path := "/knowledge-base/" + knowledgeTestID + "/data-collection"
	if !validKnowledgeDirectoryURL("https://aibase-preview.ouc-online.com.cn"+path, knowledgeTestID) {
		t.Fatal("test portal directory was rejected")
	}
	for _, raw := range []string{
		"https://ai.ouchn.edu.cn" + path,
		"https://aibase-preview.ouc-online.com.cn.evil.example" + path,
		"https://aibase-preview.ouc-online.com.cn@evil.example" + path,
		"https://aibase-preview.ouc-online.com.cn:443" + path,
		"http://aibase-preview.ouc-online.com.cn" + path,
		"https://aibase-preview.ouc-online.com.cn" + path + "?token=secret",
	} {
		if validKnowledgeDirectoryURL(raw, knowledgeTestID) {
			t.Errorf("directory outside configured portal accepted: %s", raw)
		}
	}
}
