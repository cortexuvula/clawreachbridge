package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/logring"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
)

func testDeps() Dependencies {
	p := proxy.New()
	h := proxy.NewHandler(config.DefaultConfig(), p, nil, nil)
	ring := logring.NewRingBuffer(100)

	return Dependencies{
		Proxy:      p,
		Handler:    h,
		RingBuffer: ring,
		Version:    "1.0.0-test",
		BuildTime:  "2025-01-01T00:00:00Z",
		GitCommit:  "abc1234",
		GatewayURL: "http://127.0.0.1:1", // unreachable on purpose
		StartTime:  time.Now(),
		GetConfig:  func() *config.Config { return h.GetConfig() },
		ReloadFunc: func() error { return nil },
	}
}

func TestStatusEndpoint(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp statusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Version != "1.0.0-test" {
		t.Errorf("version = %q, want %q", resp.Version, "1.0.0-test")
	}
	if resp.ActiveConnections != 0 {
		t.Errorf("active_connections = %d, want 0", resp.ActiveConnections)
	}
}

func TestStatusMethodNotAllowed(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestConnectionsEndpoint(t *testing.T) {
	deps := testDeps()
	deps.Proxy.TryIncrementConnections("10.0.0.1", 1000, 100)
	deps.Proxy.TryIncrementConnections("10.0.0.1", 1000, 100)
	deps.Proxy.TryIncrementConnections("10.0.0.2", 1000, 100)

	ui := New(deps)
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []connectionEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Sorted by count desc
	if entries[0].IP != "10.0.0.1" || entries[0].Count != 2 {
		t.Errorf("entries[0] = %+v, want {IP:10.0.0.1 Count:2}", entries[0])
	}
}

func TestConfigGetEndpoint(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp configResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Reloadable.MaxConnections != 1000 {
		t.Errorf("max_connections = %d, want 1000", resp.Reloadable.MaxConnections)
	}
	if resp.ReadOnly.TailscaleOnly != true {
		t.Error("tailscale_only should be true")
	}
}

func TestConfigPutEndpoint(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	body := `{"log_level":"debug","max_connections":500}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify config was updated
	cfg := ui.deps.GetConfig()
	if cfg.Logging.Level != "debug" {
		t.Errorf("log level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Security.MaxConnections != 500 {
		t.Errorf("max_connections = %d, want 500", cfg.Security.MaxConnections)
	}
}

func TestConfigPutBadContentType(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{}`))
	// No Content-Type header
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestConfigPutValidation(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	body := `{"log_level":"invalid"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLogsEndpoint(t *testing.T) {
	deps := testDeps()
	deps.RingBuffer.Add(logring.LogEntry{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "test message",
	})

	ui := New(deps)
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?level=info&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []logEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "test message" {
		t.Errorf("message = %q, want %q", entries[0].Message, "test message")
	}
}

func TestLogsSinceFilter(t *testing.T) {
	deps := testDeps()
	deps.RingBuffer.Add(logring.LogEntry{
		Time:    time.Now().Add(-10 * time.Minute),
		Level:   slog.LevelInfo,
		Message: "old",
	})
	deps.RingBuffer.Add(logring.LogEntry{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "new",
	})

	ui := New(deps)
	mux := ui.APIHandler()

	since := time.Now().Add(-1 * time.Minute).Format(time.RFC3339Nano)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?since="+since, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var entries []logEntryResponse
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "new" {
		t.Errorf("message = %q, want %q", entries[0].Message, "new")
	}
}

func TestReloadEndpoint(t *testing.T) {
	deps := testDeps()
	reloadCalled := false
	deps.ReloadFunc = func() error {
		reloadCalled = true
		return nil
	}

	ui := New(deps)
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reload", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if !reloadCalled {
		t.Error("reload function was not called")
	}
}

func TestReloadWrongMethod(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reload", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestStaticHandler(t *testing.T) {
	ui := New(testDeps())
	handler := ui.StaticHandler()

	req := httptest.NewRequest(http.MethodGet, "/ui/style.css", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "--bg:") {
		t.Error("response should contain CSS variables")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options header")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options header")
	}
}

func TestStaticHandlerRoot(t *testing.T) {
	ui := New(testDeps())
	handler := ui.StaticHandler()

	// /ui/ should serve index.html
	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireJSON(t *testing.T) {
	ui := New(testDeps())
	mux := ui.APIHandler()

	// Restart without Content-Type
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restart", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}
