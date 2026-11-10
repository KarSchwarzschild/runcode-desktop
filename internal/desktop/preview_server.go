package desktop

import (
	"net"
	"net/http"
	"path/filepath"
	"time"
)

// previewServer shares a loopback listener, never authority. Its injected handler
// checks source references and individual resources; no handler serves no files.
type previewServer struct {
	handler http.Handler
	srv     *http.Server
	ln      net.Listener
}

func newPreviewServer() *previewServer { return &previewServer{} }

// start opens a GET/HEAD listener; the handler owns all file authority.
func (p *previewServer) start(workspace string) (string, error) {
	if _, err := filepath.Abs(workspace); err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	slots := make(chan struct{}, 4)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if p.handler == nil {
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "preview busy", http.StatusServiceUnavailable)
			return
		}
		p.handler.ServeHTTP(w, r)
	})
	p.ln = ln
	p.srv = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 20 * time.Second}
	srv := p.srv
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String() + "/", nil
}

func (p *previewServer) stop() {
	if p.srv != nil {
		_ = p.srv.Close()
		p.srv = nil
		p.ln = nil
	}
}
