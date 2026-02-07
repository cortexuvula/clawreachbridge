//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/health"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
	"github.com/cortexuvula/clawreachbridge/internal/security"
	"golang.org/x/time/rate"
)

// newTestSetup creates a fake Gateway (echo server), Bridge proxy, and health endpoint.
func newTestSetup(t *testing.T, modCfg func(*config.Config)) (*httptest.Server, *httptest.Server, *httptest.Server) {
	t.Helper()

	// 1. Fake Gateway — echoes WebSocket messages back
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // skip Origin check in test
		})
		if err != nil {
			t.Logf("gateway accept error: %v", err)
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

	// 2. Configure bridge
	cfg := config.DefaultConfig()
	cfg.Bridge.GatewayURL = gateway.URL
	cfg.Bridge.ListenAddress = "127.0.0.1:0" // any port
	cfg.Security.TailscaleOnly = false        // disable for testing
	cfg.Security.RateLimit.Enabled = false
	cfg.Bridge.WriteTimeout = 5 * time.Second
	cfg.Bridge.DialTimeout = 5 * time.Second

	if modCfg != nil {
		modCfg(cfg)
	}

	p := proxy.New()
	var rl *security.RateLimiter
	if cfg.Security.RateLimit.Enabled {
		r := rate.Limit(float64(cfg.Security.RateLimit.ConnectionsPerMinute) / 60.0)
		rl = security.NewRateLimiter(r, cfg.Security.RateLimit.ConnectionsPerMinute)
		t.Cleanup(rl.Stop)
	}

	handler := proxy.NewHandler(cfg, p, rl)
	bridge := httptest.NewServer(handler)

	// 3. Health endpoint
	healthHandler := health.NewHandler(p, gateway.URL, "test")
	healthMux := http.NewServeMux()
	healthMux.Handle("/health", healthHandler)
	healthSrv := httptest.NewServer(healthMux)

	t.Cleanup(func() {
		bridge.Close()
		gateway.Close()
		healthSrv.Close()
	})

	return gateway, bridge, healthSrv
}

func TestProxyEcho(t *testing.T) {
	_, bridge, _ := newTestSetup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect to bridge
	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer c.CloseNow()

	// Send a message
	msg := []byte(`{"type":"ping","id":1}`)
	if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read echoed response
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(data) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", data, msg)
	}
}

func TestProxyMultipleMessages(t *testing.T) {
	_, bridge, _ := newTestSetup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	for i := 0; i < 10; i++ {
		msg := []byte(`{"seq":` + strings.Repeat("1", i+1) + `}`)
		if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if string(data) != string(msg) {
			t.Errorf("message %d: got %q, want %q", i, data, msg)
		}
	}
}

func TestProxyBinaryMessages(t *testing.T) {
	_, bridge, _ := newTestSetup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	msg := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := c.Write(ctx, websocket.MessageBinary, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Errorf("message type = %v, want Binary", typ)
	}
	if string(data) != string(msg) {
		t.Errorf("binary mismatch")
	}
}

func TestProxyAuthTokenRequired(t *testing.T) {
	_, bridge, _ := newTestSetup(t, func(cfg *config.Config) {
		cfg.Security.AuthToken = "test-secret"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")

	// Without token — should fail
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected error without auth token")
	}

	// With correct token via header
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer test-secret"},
		},
	})
	if err != nil {
		t.Fatalf("dial with token: %v", err)
	}
	c.CloseNow()

	// With correct token via query param
	c2, _, err := websocket.Dial(ctx, wsURL+"?token=test-secret", nil)
	if err != nil {
		t.Fatalf("dial with query token: %v", err)
	}
	c2.CloseNow()

	// With wrong token
	_, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer wrong-token"},
		},
	})
	if err == nil {
		t.Fatal("expected error with wrong token")
	}
}

func TestProxyConnectionLimits(t *testing.T) {
	_, bridge, _ := newTestSetup(t, func(cfg *config.Config) {
		cfg.Security.MaxConnections = 2
		cfg.Security.MaxConnectionsPerIP = 2
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")

	// Open max connections
	var conns []*websocket.Conn
	for i := 0; i < 2; i++ {
		c, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	// Next connection should be rejected
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected error when max connections reached")
	}

	// Close one and try again
	conns[0].CloseNow()
	time.Sleep(50 * time.Millisecond) // let cleanup goroutine run

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial after close: %v", err)
	}
	c.CloseNow()

	for _, conn := range conns[1:] {
		conn.CloseNow()
	}
}

func TestProxyRateLimiting(t *testing.T) {
	_, bridge, _ := newTestSetup(t, func(cfg *config.Config) {
		cfg.Security.RateLimit.Enabled = true
		cfg.Security.RateLimit.ConnectionsPerMinute = 2
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(bridge.URL, "http")

	// First two connections should succeed (burst)
	c1, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	c1.CloseNow()

	c2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	c2.CloseNow()

	// Third should be rate limited
	_, _, err = websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
}

func TestHealthEndpoint(t *testing.T) {
	_, _, healthSrv := newTestSetup(t, nil)

	resp, err := http.Get(healthSrv.URL + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}

	var hr health.Response
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if hr.Status != "ok" {
		t.Errorf("health status = %q, want %q", hr.Status, "ok")
	}
	if hr.Version != "test" {
		t.Errorf("version = %q, want %q", hr.Version, "test")
	}
}
