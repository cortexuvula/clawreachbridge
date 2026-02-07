package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), nil)

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

	handler := NewHandler(cfg, New(), rl)

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
	p.IncrementConnections("127.0.0.1") // fill the slot

	handler := NewHandler(cfg, p, nil)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
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
	p.IncrementConnections("127.0.0.1") // fill the per-IP slot

	handler := NewHandler(cfg, p, nil)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	p.DecrementConnections("127.0.0.1")
}

func TestHandlerBadRemoteAddr(t *testing.T) {
	cfg := testConfig()

	handler := NewHandler(cfg, New(), nil)

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
	handler := NewHandler(cfg, New(), nil)

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
