package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/logring"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
	"github.com/cortexuvula/clawreachbridge/internal/security"
)

//go:embed static
var staticFiles embed.FS

// Dependencies holds all injected dependencies for the web UI.
type Dependencies struct {
	Proxy       *proxy.Proxy
	Handler     *proxy.Handler
	RateLimiter *security.RateLimiter
	RingBuffer  *logring.RingBuffer
	Version     string
	BuildTime   string
	GitCommit   string
	GatewayURL  string
	StartTime   time.Time
	ReloadFunc  func() error
	GetConfig   func() *config.Config
}

// WebUI provides HTTP handlers for the admin interface.
type WebUI struct {
	deps Dependencies
}

// New creates a new WebUI instance.
func New(deps Dependencies) *WebUI {
	return &WebUI{deps: deps}
}

// StaticHandler returns an http.Handler serving embedded static files at /ui/.
func (ui *WebUI) StaticHandler() http.Handler {
	sub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(sub))
	return securityHeaders(http.StripPrefix("/ui/", fileServer))
}

// APIHandler returns an http.Handler for /api/v1/ endpoints.
func (ui *WebUI) APIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", ui.handleStatus)
	mux.HandleFunc("/api/v1/connections", ui.handleConnections)
	mux.HandleFunc("/api/v1/config", ui.handleConfig)
	mux.HandleFunc("/api/v1/logs", ui.handleLogs)
	mux.HandleFunc("/api/v1/reload", ui.handleReload)
	mux.HandleFunc("/api/v1/restart", ui.handleRestart)
	return mux
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}
