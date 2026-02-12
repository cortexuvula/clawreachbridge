package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/security"
	"golang.org/x/time/rate"
)

func testConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Bridge.GatewayURL = "http://127.0.0.1:19999" // nothing listening
	cfg.Bridge.ListenAddress = "127.0.0.1:0"
	cfg.Security.TailscaleOnly = false
	cfg.Security.RateLimit.Enabled = false
	cfg.Bridge.WriteTimeout = 5 * time.Second
	cfg.Bridge.DialTimeout = 5 * time.Second
	return cfg
}

func TestHandlerRejectNonTailscaleIP(t *testing.T) {
	cfg := testConfig()
	cfg.Security.TailscaleOnly = true

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345" // not a Tailscale IP
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerAllowTailscaleIP(t *testing.T) {
	cfg := testConfig()
	cfg.Security.TailscaleOnly = true

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "100.64.0.1:12345" // Tailscale IP
	rec := httptest.NewRecorder()

	// This will fail at Accept (not a real WebSocket) but should get past the IP check
	handler.ServeHTTP(rec, req)

	// Should NOT be 403 — it'll fail later at WebSocket accept
	if rec.Code == http.StatusForbidden {
		t.Errorf("Tailscale IP should not be rejected")
	}
}

func TestHandlerRejectMissingAuthToken(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerRejectWrongAuthToken(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerAcceptCorrectAuthToken(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should NOT be 403 — it'll fail later at WebSocket accept
	if rec.Code == http.StatusForbidden {
		t.Errorf("correct auth token should not be rejected")
	}
}

func TestHandlerAcceptQueryParamToken(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/?token=secret-token", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Errorf("correct query param token should not be rejected")
	}
}

func TestHandlerRejectRateLimited(t *testing.T) {
	cfg := testConfig()
	cfg.Security.RateLimit.Enabled = true
	cfg.Security.RateLimit.ConnectionsPerMinute = 1

	r := rate.Limit(float64(cfg.Security.RateLimit.ConnectionsPerMinute) / 60.0)
	rl := security.NewRateLimiter(r, 1) // burst of 1
	defer rl.Stop()

	handler := NewHandler(cfg, New(), rl, context.Background())

	// First request — uses the one token in the bucket
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request — should be rate limited
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}
}

func TestHandlerRejectMaxConnections(t *testing.T) {
	cfg := testConfig()
	cfg.Security.MaxConnections = 1

	p := New()
	p.TryIncrementConnections("127.0.0.1", 1000, 100) // fill the slot

	handler := NewHandler(cfg, p, nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	p.DecrementConnections("127.0.0.1")
}

func TestHandlerRejectMaxConnectionsPerIP(t *testing.T) {
	cfg := testConfig()
	cfg.Security.MaxConnectionsPerIP = 1

	p := New()
	p.TryIncrementConnections("127.0.0.1", 1000, 100) // fill the per-IP slot

	handler := NewHandler(cfg, p, nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	p.DecrementConnections("127.0.0.1")
}

func TestHandlerBadRemoteAddr(t *testing.T) {
	cfg := testConfig()

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "no-port-here" // invalid, no port
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlerUpdateConfig(t *testing.T) {
	cfg := testConfig()
	handler := NewHandler(cfg, New(), nil, context.Background())

	// Original config has no auth token
	if handler.GetConfig().Security.AuthToken != "" {
		t.Error("expected empty auth token initially")
	}

	// Update config
	newCfg := testConfig()
	newCfg.Security.AuthToken = "new-secret"
	handler.UpdateConfig(newCfg)

	if handler.GetConfig().Security.AuthToken != "new-secret" {
		t.Error("expected updated auth token")
	}
}

// echoGateway creates a test WebSocket echo server (fake Gateway).
func echoGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			msgType, reader, err := c.Reader(r.Context())
			if err != nil {
				return
			}
			writer, err := c.Writer(r.Context(), msgType)
			if err != nil {
				return
			}
			if _, err := io.Copy(writer, reader); err != nil {
				return
			}
			writer.Close()
		}
	}))
}

// setupBridgeWithGateway creates a bridge+gateway pair for WebSocket-level tests.
func setupBridgeWithGateway(t *testing.T) (*httptest.Server, *Handler, *Proxy) {
	t.Helper()
	gw := echoGateway(t)
	t.Cleanup(gw.Close)

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gw.URL
	cfg.Bridge.PingInterval = 0 // disable keepalive for these tests

	p := New()
	handler := NewHandler(cfg, p, nil, context.Background())
	bridge := httptest.NewServer(handler)
	t.Cleanup(bridge.Close)

	// Stash bridge URL on the handler config so tests can connect
	cfg.Bridge.ListenAddress = bridge.Listener.Addr().String()

	return bridge, handler, p
}

func TestGracefulClose(t *testing.T) {
	bridge, _, _ := setupBridgeWithGateway(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	// Send a message and read the echo to confirm the connection works
	if err := c.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("echo mismatch: got %q", data)
	}

	// Close the bridge server — this triggers connection cleanup.
	// The client should receive a close frame (CloseError), not a raw EOF.
	bridge.Close()

	// Try to read — should get a close error, not io.EOF
	_, _, err = c.Read(ctx)
	if err == nil {
		t.Fatal("expected error after bridge close")
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		// When the server's HTTP transport tears down the connection abruptly,
		// we may get io.EOF instead of a close frame. That's acceptable for
		// httptest.Server.Close() which doesn't trigger our drain path.
		// The important thing is we don't hang.
		t.Logf("got non-close error (acceptable for httptest.Close): %v", err)
	} else {
		t.Logf("received close frame: code=%d reason=%q", closeErr.Code, closeErr.Reason)
	}
}

func TestHandlerHTTPProxy(t *testing.T) {
	// Start a fake gateway HTTP server that returns known content.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>A2UI Canvas</html>"))
	}))
	defer gateway.Close()

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gateway.URL

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if body != "<html>A2UI Canvas</html>" {
		t.Errorf("body = %q, want %q", body, "<html>A2UI Canvas</html>")
	}
}

func TestHandlerHTTPProxyPreservesPath(t *testing.T) {
	var receivedPath, receivedQuery string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gateway.URL

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/__openclaw__/a2ui/?platform=android", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if receivedPath != "/__openclaw__/a2ui/" {
		t.Errorf("path = %q, want %q", receivedPath, "/__openclaw__/a2ui/")
	}
	if receivedQuery != "platform=android" {
		t.Errorf("query = %q, want %q", receivedQuery, "platform=android")
	}
}

func TestHandlerHTTPProxyRejectNonTailscale(t *testing.T) {
	cfg := testConfig()
	cfg.Security.TailscaleOnly = true

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/page", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerHTTPProxyRejectBadAuth(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/page", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerHTTPProxyGatewayDown(t *testing.T) {
	cfg := testConfig()
	cfg.Bridge.GatewayURL = "http://127.0.0.1:19999" // nothing listening

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/page", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandlerHTTPProxyInjectsOrigin(t *testing.T) {
	var receivedOrigin string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedOrigin = r.Header.Get("Origin")
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gateway.URL
	cfg.Bridge.Origin = "https://my-gateway.local"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/__openclaw__/a2ui/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if receivedOrigin != "https://my-gateway.local" {
		t.Errorf("Origin = %q, want %q", receivedOrigin, "https://my-gateway.local")
	}
}

func TestDrainOnShutdown(t *testing.T) {
	gw := echoGateway(t)
	t.Cleanup(gw.Close)

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gw.URL
	cfg.Bridge.PingInterval = 0

	p := New()
	handler := NewHandler(cfg, p, nil, context.Background())
	bridge := httptest.NewServer(handler)
	defer bridge.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	// Confirm connection is live
	if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "ping" {
		t.Fatalf("echo mismatch: got %q", data)
	}

	// Trigger drain — this should send a close frame to the client
	handler.StartDrain()

	// Client should receive a close frame with StatusGoingAway
	_, _, err = c.Read(ctx)
	if err == nil {
		t.Fatal("expected error after drain")
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected CloseError, got: %v", err)
	}
	if closeErr.Code != websocket.StatusGoingAway {
		t.Errorf("close code = %d, want %d (StatusGoingAway)", closeErr.Code, websocket.StatusGoingAway)
	}
	if closeErr.Reason != "server shutting down" {
		t.Errorf("close reason = %q, want %q", closeErr.Reason, "server shutting down")
	}

	// Connection count should drop to 0 after drain
	time.Sleep(100 * time.Millisecond)
	if count := p.ConnectionCount(); count != 0 {
		t.Errorf("connection count = %d after drain, want 0", count)
	}
}

func TestHandlerPublicPathBypassesAuth(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gateway.URL
	cfg.Security.AuthToken = "secret-token"
	// default public_paths includes /__openclaw__/a2ui/

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/__openclaw__/a2ui/?platform=android", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	// No Authorization header — should still pass
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (public path should bypass auth)", rec.Code, http.StatusOK)
	}
}

func TestHandlerPublicPathStillRequiresTailscale(t *testing.T) {
	cfg := testConfig()
	cfg.Security.TailscaleOnly = true
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/__openclaw__/a2ui/?platform=android", nil)
	req.RemoteAddr = "192.168.1.1:12345" // not a Tailscale IP
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (public path should still require Tailscale)", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerNonPublicPathStillRequiresAuth(t *testing.T) {
	cfg := testConfig()
	cfg.Security.AuthToken = "secret-token"

	handler := NewHandler(cfg, New(), nil, context.Background())

	req := httptest.NewRequest("GET", "/other-path", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (non-public path should require auth)", rec.Code, http.StatusForbidden)
	}
}

func TestHandlerPublicPathCustomList(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := testConfig()
	cfg.Bridge.GatewayURL = gateway.URL
	cfg.Security.AuthToken = "secret-token"
	cfg.Security.PublicPaths = []string{"/static/", "/public/"}

	handler := NewHandler(cfg, New(), nil, context.Background())

	// /static/ prefix matches
	req := httptest.NewRequest("GET", "/static/app.js", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/static/app.js: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// /public/ prefix matches
	req2 := httptest.NewRequest("GET", "/public/index.html", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("/public/index.html: status = %d, want %d", rec2.Code, http.StatusOK)
	}

	// /__openclaw__/a2ui/ NOT in custom list — should require auth
	req3 := httptest.NewRequest("GET", "/__openclaw__/a2ui/?platform=android", nil)
	req3.RemoteAddr = "127.0.0.1:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusForbidden {
		t.Errorf("/__openclaw__/a2ui/: status = %d, want %d (not in custom public_paths)", rec3.Code, http.StatusForbidden)
	}
}

func TestShouldInjectMedia(t *testing.T) {
	tests := []struct {
		name        string
		injectPaths []string
		reqPath     string
		want        bool
	}{
		{"empty paths injects everywhere", nil, "/ws/node", true},
		{"empty paths injects root", nil, "/", true},
		{"matching prefix", []string{"/ws/operator"}, "/ws/operator", true},
		{"matching prefix with subpath", []string{"/ws/operator"}, "/ws/operator/session/123", true},
		{"non-matching path", []string{"/ws/operator"}, "/ws/node", false},
		{"multiple prefixes match first", []string{"/ws/operator", "/ws/chat"}, "/ws/operator", true},
		{"multiple prefixes match second", []string{"/ws/operator", "/ws/chat"}, "/ws/chat/session", true},
		{"multiple prefixes no match", []string{"/ws/operator", "/ws/chat"}, "/ws/node", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Bridge.Media.InjectPaths = tt.injectPaths
			handler := NewHandler(cfg, New(), nil, context.Background())

			got := handler.shouldInjectMedia(tt.reqPath)
			if got != tt.want {
				t.Errorf("shouldInjectMedia(%q) = %v, want %v (inject_paths=%v)", tt.reqPath, got, tt.want, tt.injectPaths)
			}
		})
	}
}
