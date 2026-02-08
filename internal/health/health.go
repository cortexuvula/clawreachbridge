package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/metrics"
	"github.com/cortexuvula/clawreachbridge/internal/proxy"
)

// Response is the JSON response from the /health endpoint.
type Response struct {
	Status            string   `json:"status"`
	Uptime            string   `json:"uptime"`
	ActiveConnections int      `json:"active_connections"`
	GatewayReachable  bool     `json:"gateway_reachable"`
	Version           string   `json:"version"`
	Timestamp         string   `json:"timestamp"`
	Details           *Details `json:"details,omitempty"`
}

// Details contains extended health information.
type Details struct {
	TotalConnections int64   `json:"total_connections"`
	TotalMessages    int64   `json:"total_messages"`
	MemoryMB         float64 `json:"memory_mb"`
}

// Handler serves the health check endpoint.
type Handler struct {
	startTime  time.Time
	proxy      *proxy.Proxy
	metrics    *metrics.Metrics // optional, nil if metrics disabled
	gatewayURL string
	version    string
	detailed   bool
}

// NewHandler creates a new health check handler.
func NewHandler(p *proxy.Proxy, gatewayURL, version string, detailed bool) *Handler {
	return &Handler{
		startTime:  time.Now(),
		proxy:      p,
		gatewayURL: gatewayURL,
		version:    version,
		detailed:   detailed,
	}
}

// SetMetrics sets the optional Prometheus metrics.
func (h *Handler) SetMetrics(m *metrics.Metrics) {
	h.metrics = m
}

// ServeHTTP handles health check requests.
// Health listener runs on 127.0.0.1:8081 (separate from proxy listener).
// This allows local monitoring tools (systemd, Prometheus, Nagios) to check
// health without being on the Tailscale network.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gatewayOK := h.checkGateway()

	if h.metrics != nil {
		if gatewayOK {
			h.metrics.GatewayReachable.Set(1)
		} else {
			h.metrics.GatewayReachable.Set(0)
		}
	}

	status := "ok"
	httpCode := http.StatusOK
	if !gatewayOK {
		status = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	resp := Response{
		Status:            status,
		Uptime:            time.Since(h.startTime).Round(time.Second).String(),
		ActiveConnections: h.proxy.ConnectionCount(),
		GatewayReachable:  gatewayOK,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
	}

	if h.detailed {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		resp.Version = h.version
		resp.Details = &Details{
			TotalConnections: h.proxy.TotalConnections(),
			TotalMessages:    h.proxy.TotalMessages(),
			MemoryMB:         float64(memStats.Alloc) / 1024 / 1024,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	json.NewEncoder(w).Encode(resp)
}

// checkGateway verifies the upstream Gateway is reachable.
// Uses a plain HTTP request (not WebSocket dial) to avoid creating real
// connections and polluting Gateway logs on every health poll.
// noRedirectClient refuses to follow HTTP redirects to prevent SSRF amplification.
var noRedirectClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (h *Handler) checkGateway() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.gatewayURL, nil)
	if err != nil {
		slog.Debug("gateway health check request creation failed", "error", err)
		return false
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		slog.Debug("gateway unreachable", "url", h.gatewayURL, "error", err)
		return false
	}
	resp.Body.Close()
	return true // any response (even 4xx/3xx) means Gateway is alive
}
