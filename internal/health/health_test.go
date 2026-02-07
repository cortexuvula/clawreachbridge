package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cortexuvula/clawreachbridge/internal/proxy"
)

func TestHealthHandler_Healthy(t *testing.T) {
	// Set up a fake gateway
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	p := proxy.New()
	h := NewHandler(p, gateway.URL, "test-version")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
	if !resp.GatewayReachable {
		t.Error("gateway_reachable should be true")
	}
	if resp.Version != "test-version" {
		t.Errorf("version = %q, want %q", resp.Version, "test-version")
	}
	if resp.ActiveConnections != 0 {
		t.Errorf("active_connections = %d, want 0", resp.ActiveConnections)
	}
	if resp.Details == nil {
		t.Error("details should not be nil")
	}
}

func TestHealthHandler_GatewayDown(t *testing.T) {
	p := proxy.New()
	// Point to an address that won't respond
	h := NewHandler(p, "http://127.0.0.1:1", "test-version")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("status = %q, want %q", resp.Status, "degraded")
	}
	if resp.GatewayReachable {
		t.Error("gateway_reachable should be false")
	}
}

func TestHealthHandler_WithConnections(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	p := proxy.New()
	p.IncrementConnections("100.64.0.1")
	p.IncrementConnections("100.64.0.2")

	h := NewHandler(p, gateway.URL, "test-version")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ActiveConnections != 2 {
		t.Errorf("active_connections = %d, want 2", resp.ActiveConnections)
	}
}

func TestHealthHandler_Gateway4xx(t *testing.T) {
	// Gateway returns 404 — should still be considered alive
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gateway.Close()

	p := proxy.New()
	h := NewHandler(p, gateway.URL, "test-version")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.GatewayReachable {
		t.Error("gateway returning 4xx should still be reachable")
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}
