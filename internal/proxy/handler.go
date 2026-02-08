package proxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/metrics"
	"github.com/cortexuvula/clawreachbridge/internal/security"
	"golang.org/x/time/rate"
)

// Handler is the HTTP handler that accepts WebSocket connections from
// ClawReach clients and proxies them to the OpenClaw Gateway.
type Handler struct {
	Config      *config.Config
	Proxy       *Proxy
	RateLimiter *security.RateLimiter
	Metrics     *metrics.Metrics // optional, nil if metrics disabled
	ShutdownCtx context.Context  // cancelled on server shutdown

	// mu protects Config during hot-reload
	mu sync.RWMutex
}

// NewHandler creates a new proxy handler.
func NewHandler(cfg *config.Config, p *Proxy, rl *security.RateLimiter, shutdownCtx context.Context) *Handler {
	return &Handler{
		Config:      cfg,
		Proxy:       p,
		RateLimiter: rl,
		ShutdownCtx: shutdownCtx,
	}
}

// GetConfig returns the current config (thread-safe for hot-reload).
func (h *Handler) GetConfig() *config.Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.Config
}

// UpdateConfig swaps the config (called on SIGHUP).
func (h *Handler) UpdateConfig(cfg *config.Config) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Config = cfg
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cfg := h.GetConfig()

	// 1. Validate Tailscale IP
	if cfg.Security.TailscaleOnly && !security.IsTailscaleIP(r.RemoteAddr) {
		slog.Warn("rejected non-Tailscale connection", "remote_addr", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// 2. Parse client IP (needed for auth logging, rate limiting, and connection tracking)
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		slog.Error("failed to parse remote address", "remote_addr", r.RemoteAddr, "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// 3. Optional auth token check (header first, query param fallback)
	if cfg.Security.AuthToken != "" {
		token := security.ExtractBearerToken(r.Header.Get("Authorization"))
		if token == "" {
			token = r.URL.Query().Get("token")
			if token != "" {
				slog.Warn("auth token provided via query parameter; use Authorization header instead", "client_ip", clientIP)
			}
		}
		if !security.TokenMatch(token, cfg.Security.AuthToken) {
			slog.Warn("rejected invalid auth token", "client_ip", clientIP)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// 4. Rate limit check
	if cfg.Security.RateLimit.Enabled && h.RateLimiter != nil && !h.RateLimiter.Allow(clientIP) {
		slog.Warn("rate limit exceeded", "client_ip", clientIP)
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	// 5. Connection limits (atomic check-and-increment to prevent TOCTOU race)
	if reason := h.Proxy.TryIncrementConnections(clientIP, cfg.Security.MaxConnections, cfg.Security.MaxConnectionsPerIP); reason != "" {
		if reason == "max_connections" {
			slog.Warn("max connections reached", "current", h.Proxy.ConnectionCount(), "max", cfg.Security.MaxConnections)
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		} else {
			slog.Warn("max connections per IP reached", "client_ip", clientIP, "current", h.Proxy.ConnectionCountForIP(clientIP))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		}
		return
	}
	if h.Metrics != nil {
		h.Metrics.ConnectionsTotal.Inc()
		h.Metrics.ActiveConnections.Inc()
	}

	// 6. Accept client WebSocket connection
	// Forward subprotocols from client request to Gateway
	subprotocols := r.Header.Values("Sec-WebSocket-Protocol")

	// Filter subprotocols if an allowlist is configured
	if len(cfg.Bridge.AllowedSubprotocols) > 0 {
		allowed := make(map[string]bool, len(cfg.Bridge.AllowedSubprotocols))
		for _, sp := range cfg.Bridge.AllowedSubprotocols {
			allowed[sp] = true
		}
		var filtered []string
		for _, sp := range subprotocols {
			if allowed[sp] {
				filtered = append(filtered, sp)
			}
		}
		if len(subprotocols) > 0 && len(filtered) == 0 {
			h.Proxy.DecrementConnections(clientIP)
			if h.Metrics != nil {
				h.Metrics.ActiveConnections.Dec()
				h.Metrics.ErrorsTotal.WithLabelValues("subprotocol_rejected").Inc()
			}
			slog.Warn("rejected connection: no allowed subprotocols", "client_ip", clientIP, "requested", subprotocols)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		subprotocols = filtered
	}
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: subprotocols,
	})
	if err != nil {
		h.Proxy.DecrementConnections(clientIP)
		if h.Metrics != nil {
			h.Metrics.ActiveConnections.Dec()
			h.Metrics.ErrorsTotal.WithLabelValues("accept_failure").Inc()
		}
		slog.Error("failed to accept client WebSocket", "error", err)
		return
	}
	clientConn.SetReadLimit(cfg.Bridge.MaxMessageSize)

	// 7. Dial Gateway with Origin header and matching subprotocols
	dialCtx, dialCancel := context.WithTimeout(r.Context(), cfg.Bridge.DialTimeout)
	defer dialCancel()

	gatewayURL := httpToWS(cfg.Bridge.GatewayURL)
	gatewayConn, _, err := websocket.Dial(dialCtx, gatewayURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": {cfg.Bridge.Origin}},
		Subprotocols: subprotocols,
	})
	if err != nil {
		slog.Error("failed to dial gateway", "url", gatewayURL, "error", err)
		clientConn.Close(websocket.StatusBadGateway, "gateway unreachable")
		h.Proxy.DecrementConnections(clientIP)
		if h.Metrics != nil {
			h.Metrics.ActiveConnections.Dec()
			h.Metrics.ErrorsTotal.WithLabelValues("dial_failure").Inc()
		}
		return
	}
	gatewayConn.SetReadLimit(cfg.Bridge.MaxMessageSize)

	slog.Info("connection established", "client_ip", clientIP, "gateway", gatewayURL)

	// 8. Bidirectional forwarding with coordinated shutdown
	// When either direction finishes, cancel context to tear down the other side.
	// context.CancelFunc is safe to call multiple times.
	proxyCtx, proxyCancel := context.WithCancel(h.ShutdownCtx)

	// Start keepalive pings to detect dead connections.
	// Ping must run concurrently with Reader per coder/websocket docs.
	if cfg.Bridge.PingInterval > 0 {
		go h.keepAlive(proxyCtx, clientConn, cfg.Bridge.PingInterval, cfg.Bridge.PongTimeout, proxyCancel)
		go h.keepAlive(proxyCtx, gatewayConn, cfg.Bridge.PingInterval, cfg.Bridge.PongTimeout, proxyCancel)
	}

	// Guard CloseNow with sync.Once — context cancellation can trigger
	// internal closes in coder/websocket concurrently with our cleanup.
	var closeClientOnce, closeGatewayOnce sync.Once
	closeClient := func() { closeClientOnce.Do(func() { clientConn.CloseNow() }) }
	closeGateway := func() { closeGatewayOnce.Do(func() { gatewayConn.CloseNow() }) }

	// Per-connection message rate limiter (client→gateway only)
	var msgLimiter *rate.Limiter
	if cfg.Security.RateLimit.Enabled && cfg.Security.RateLimit.MessagesPerSecond > 0 {
		msgLimiter = rate.NewLimiter(rate.Limit(cfg.Security.RateLimit.MessagesPerSecond), cfg.Security.RateLimit.MessagesPerSecond)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer proxyCancel()
		h.forwardMessages(proxyCtx, clientConn, gatewayConn, "client→gateway", msgLimiter)
	}()
	go func() {
		defer wg.Done()
		defer proxyCancel()
		h.forwardMessages(proxyCtx, gatewayConn, clientConn, "gateway→client", nil)
	}()

	// Cleanup: wait for both to finish, then close connections
	go func() {
		start := time.Now()
		wg.Wait()
		closeClient()
		closeGateway()
		h.Proxy.DecrementConnections(clientIP)
		if h.Metrics != nil {
			h.Metrics.ActiveConnections.Dec()
		}
		slog.Info("connection closed", "client_ip", clientIP, "duration", time.Since(start).String())
	}()
}

// forwardMessages reads from src and writes to dst until the context is
// cancelled or either side closes. This is the core proxy loop.
// direction is "client→gateway" or "gateway→client" for logging.
// msgLimiter is optional; if non-nil, messages are rate-limited.
func (h *Handler) forwardMessages(ctx context.Context, src, dst *websocket.Conn, direction string, msgLimiter *rate.Limiter) {
	cfg := h.GetConfig()
	for {
		readCtx, readCancel := context.WithTimeout(ctx, cfg.Bridge.ReadTimeout)
		msgType, reader, err := src.Reader(readCtx)
		readCancel()
		if err != nil {
			slog.Debug("forward stopped", "direction", direction, "reason", err)
			return
		}

		if msgLimiter != nil {
			if err := msgLimiter.Wait(ctx); err != nil {
				slog.Debug("message rate limit", "direction", direction, "reason", err)
				return
			}
		}

		writeCtx, writeCancel := context.WithTimeout(ctx, cfg.Bridge.WriteTimeout)
		writer, err := dst.Writer(writeCtx, msgType)
		if err != nil {
			writeCancel()
			slog.Debug("write failed", "direction", direction, "reason", err)
			return
		}

		if _, err := io.Copy(writer, reader); err != nil {
			writeCancel()
			slog.Debug("copy failed", "direction", direction, "reason", err)
			return
		}
		if err := writer.Close(); err != nil {
			writeCancel()
			slog.Debug("flush failed", "direction", direction, "reason", err)
			return
		}
		writeCancel()

		h.Proxy.IncrementMessages()
		if h.Metrics != nil {
			if direction == "client→gateway" {
				h.Metrics.MessagesTotal.WithLabelValues("upstream").Inc()
			} else {
				h.Metrics.MessagesTotal.WithLabelValues("downstream").Inc()
			}
		}
	}
}

// keepAlive sends periodic WebSocket pings to detect dead connections.
// If a ping fails or times out, it cancels the proxy context to tear down both sides.
func (h *Handler) keepAlive(ctx context.Context, conn *websocket.Conn, interval, pongTimeout time.Duration, cancel context.CancelFunc) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, pongTimeout)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				slog.Debug("keepalive ping failed, closing connection", "error", err)
				cancel()
				return
			}
		}
	}
}

// httpToWS converts http:// to ws:// and https:// to wss://.
func httpToWS(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "wss://" + strings.TrimPrefix(url, "https://")
	}
	if strings.HasPrefix(url, "http://") {
		return "ws://" + strings.TrimPrefix(url, "http://")
	}
	return url
}
