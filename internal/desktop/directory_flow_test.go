package desktop

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	engine "gitlab.ouc-online.com.cn/aibase/agentloop"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// This drives real host turns, resolver/policy/authorizer, actual ReadOffice,
// preview registration, Wails-equivalent reads and loopback HTTP. Only the
// upstream model is a deterministic local fixture; no paid model or GUI runs.
func TestDirectoryOfficePreviewActualHostFlow(t *testing.T) {
	isolateConfigDir(t)
	ws := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(outside, "资料.docx")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(f)
	part, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>permission fixture content</w:t></w:r></w:p></w:body></w:document>`))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var sawOffice atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad fixture request", 400)
			return
		}
		n := calls.Add(1)
		data, _ := json.Marshal(request)
		if strings.Contains(string(data), "permission fixture content") {
			sawOffice.Store(true)
		}
		delta := map[string]any{"content": "fixture complete"}
		finish := "stop"
		if n == 1 {
			input, _ := json.Marshal(map[string]string{"path": source})
			tools := []any{}
			for i, name := range []string{"ReadOffice", "open_preview"} {
				tools = append(tools, map[string]any{"index": i, "id": fmt.Sprintf("file-%d", i), "type": "function", "function": map[string]any{"name": name, "arguments": string(input)}})
			}
			delta = map[string]any{"tool_calls": tools}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []any{
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}},
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}},
		} {
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	sink := &recordingSink{}
	app := New(sink)
	app.startPreview(ws)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	defer func() { _ = app.mgr.CloseAll(ctx); app.stopPreview(ws) }()
	id, _, err := app.mgr.Create(ctx, engine.Config{CWD: ws, Model: "fixture", Provider: "openai", BaseURL: model.URL + "/v1", APIKey: "synthetic", PermissionMode: "safe", MaxIterations: 3})
	if err != nil {
		t.Fatal(err)
	}
	focusOn(app, id, ws)
	if _, err := app.GrantDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "session"}); err != nil {
		t.Fatal(err)
	}
	if err := app.mgr.SendMessage(id, "Read and preview the synthetic document"); err != nil {
		t.Fatal(err)
	}
	waitForTurns(t, sink, 1, time.Now().Add(15*time.Second))
	if !sawOffice.Load() {
		t.Fatal("ReadOffice did not deliver the external document text to the actual model request")
	}
	var ref ArtifactReference
	sink.mu.Lock()
	for _, event := range sink.events {
		env, ok := event.data.(hostproto.Envelope)
		if !ok || env.SessionID != id {
			continue
		}
		payload, ok := env.Payload.(hostproto.ToolEvent)
		if !ok || payload.ToolName != "open_preview" || payload.Type != "progress" {
			continue
		}
		data, _ := json.Marshal(payload.Data)
		_ = json.Unmarshal(data, &ref)
	}
	sink.mu.Unlock()
	if ref.Reference == "" || ref.Generation == "" || ref.SessionID != id {
		t.Fatalf("missing source-bound preview receipt: %+v", ref)
	}
	focusOn(app, "different-focus", t.TempDir())
	checkPreviewHTTP(t, "GET", ref.URL, 200)
	raw, err := app.ReadArtifactBytes(ref.Reference)
	if err != nil || raw == "" {
		t.Fatal("binary preview failed", err)
	}
	if _, err := app.RevokeDirectoryAccess(DirectoryAccessRequest{SessionID: id, Directory: outside, Access: "read", Scope: "session"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReadArtifactBytes(ref.Reference); err == nil {
		t.Fatal("revoked source remained readable")
	}
	checkPreviewHTTP(t, "GET", ref.URL, 403)
}
