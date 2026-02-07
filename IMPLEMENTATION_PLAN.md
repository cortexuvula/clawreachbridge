# ClawReach Bridge - Implementation Plan

## Executive Summary

ClawReach Bridge is a secure WebSocket proxy that enables ClawReach mobile/web clients to connect to OpenClaw Gateway over Tailscale without requiring changes to OpenClaw's security model. The bridge solves the WebSocket origin check issue (GitHub #9358, PR #10695) by acting as a trusted intermediary that supplies proper headers while maintaining strong security through Tailscale's encrypted mesh network.

## Architecture Overview

```
ClawReach Client (mobile/web)
        ↓ (Tailscale, encrypted)
ClawReach Bridge (trusted proxy)
        ↓ (localhost, proper Origin headers)
OpenClaw Gateway (unmodified)
```

**Key principle:** Tailscale provides the security boundary. The bridge translates Tailscale-authenticated connections into localhost connections that Gateway trusts.

## Technology Stack

### Language: Go (Golang)

**Rationale:**
- **Single static binary**: No runtime dependencies, trivial deployment
- **Cross-compilation**: Build for Linux, macOS, Windows, ARM from any platform
- **Excellent concurrency**: Goroutines handle thousands of concurrent connections efficiently
- **Strong WebSocket support**: `coder/websocket` (formerly `nhooyr.io/websocket`) is actively maintained and uses idiomatic `net/http` patterns with native `context.Context` support
- **Low resource footprint**: ~5-15MB memory, minimal CPU
- **Type safety**: Reduces bugs in network code
- **Fast compilation**: Developers get quick feedback
- **Great systemd integration**: Native support for socket activation, watchdog

**Alternative considered:**
- Node.js: Requires runtime, larger memory footprint, but easier for JS developers
- Rust: More secure but harder to compile/distribute, longer dev time
- Python: Requires interpreter, slower, but easiest to read

**Decision:** Go strikes the best balance for this use case.

## Core Features

### 1. WebSocket Proxying
- Bidirectional message forwarding between ClawReach ↔ Gateway
- Preserve all WebSocket frames (text, binary, ping, pong, close)
- Forward WebSocket subprotocols (`Sec-WebSocket-Protocol`) from client to Gateway
- Handle both secure (wss://) and insecure (ws://) upstream (auto-converts `http→ws`, `https→wss`)
- Configurable max message size (default 1MB) to prevent memory exhaustion
- Ping/pong keepalive with configurable intervals to detect dead peers
- Coordinated connection shutdown: when either side closes, the other is torn down via `context.Context`
- Automatic reconnection with exponential backoff
- Connection pooling to Gateway (optional, for multiple clients)

### 2. Security
- **Tailscale-only listening**: Bind to Tailscale interface IP only, never 0.0.0.0
- **Connection validation**: Verify client IP is in Tailscale network range (IPv4: `100.64.0.0/10`, IPv6: `fd7a:115c:a1e0::/48`)
- **Origin header injection**: Add `Origin: https://gateway.local` or configured value
- **Optional auth token**: Support `Authorization: Bearer <token>` header (primary) with `?token=xxx` query parameter fallback for development/testing. Token comparison uses `crypto/subtle.ConstantTimeCompare` to prevent timing attacks
- **TLS support**: Option to terminate TLS at bridge (though Tailscale already encrypts)
- **Rate limiting**: Per-IP connection and message rate limits (configurable)
- **Audit logging**: Log all connections with timestamps, source IPs, durations

### 3. Health & Monitoring
- **Health check endpoint**: `/health` on separate listener (`127.0.0.1:8081`) — accessible by local monitoring tools (systemd, Prometheus) without Tailscale access
- **Metrics endpoint**: `/metrics` returns Prometheus-compatible metrics (optional)
- **Structured logging**: JSON logs with levels (DEBUG, INFO, WARN, ERROR)
- **Graceful shutdown**: On SIGTERM/SIGINT, stop accepting new connections, wait for active connections to finish (up to `drain_timeout`, default 30s), then force-close remaining connections
- **Config reload**: SIGHUP triggers hot-reload of: rate limits, auth tokens, log level, max message size, connection limits. Settings that require restart: `listen_address`, `gateway_url`, `tls`, `health.listen_address`
- **Watchdog support**: systemd sd_notify for service monitoring

### 4. Configuration
- **Config file**: YAML or TOML format (prefer YAML for readability)
- **Environment variables**: Override config with env vars (12-factor app pattern). Convention: `CLAWREACH_` prefix + uppercase + underscores for nesting (e.g., `CLAWREACH_BRIDGE_LISTEN_ADDRESS`, `CLAWREACH_SECURITY_AUTH_TOKEN`, `CLAWREACH_LOGGING_LEVEL`)
- **Sensible defaults**: Works out-of-box with minimal config
- **Config validation**: Fail fast on invalid settings with clear error messages

### 5. Developer Experience
- **Easy installation**: Single curl command or dpkg/rpm package
- **Interactive setup**: `clawreachbridge setup` wizard for first-time config
- **Clear error messages**: Help users diagnose issues (e.g., "Tailscale not running")
- **Verbose mode**: `--verbose` or `-v` flag for debugging
- **Version command**: `clawreachbridge version` shows build info

## Detailed Design

### 1. Project Structure

```
clawreachbridge/
├── cmd/
│   └── clawreachbridge/
│       └── main.go                 # Entry point, CLI parsing
├── internal/
│   ├── config/
│   │   ├── config.go               # Config loading, validation
│   │   └── config_test.go
│   ├── proxy/
│   │   ├── proxy.go                # Core WebSocket proxy logic
│   │   ├── handler.go              # HTTP/WebSocket handler
│   │   ├── connection.go           # Connection management
│   │   └── proxy_test.go
│   ├── security/
│   │   ├── tailscale.go            # Tailscale IP validation
│   │   ├── ratelimit.go            # Rate limiting
│   │   └── auth.go                 # Optional token auth
│   ├── health/
│   │   └── health.go               # Health check endpoint
│   └── logging/
│       └── logger.go               # Structured logging setup
├── scripts/
│   ├── install.sh                  # Installation script (Linux/macOS)
│   ├── uninstall.sh
│   └── build.sh                    # Cross-compilation script
├── systemd/
│   └── clawreachbridge.service     # systemd unit file
├── configs/
│   └── config.example.yaml         # Example config file
├── test/
│   ├── integration/                # Integration tests (build tag: integration)
│   └── loadtest/                   # WebSocket load testing tools
├── docs/
│   ├── INSTALLATION.md             # Installation guide
│   ├── CONFIGURATION.md            # Config reference
│   ├── SECURITY.md                 # Security considerations
│   └── TROUBLESHOOTING.md          # Common issues
├── go.mod
├── go.sum
├── README.md
├── LICENSE                         # MIT or Apache 2.0
└── CHANGELOG.md
```

### 2. Configuration Schema

```yaml
# config.yaml
bridge:
  # Listen address (should be Tailscale IP)
  listen_address: "100.x.x.x:8080"
  
  # OpenClaw Gateway upstream (http:// or https://)
  # The bridge auto-converts to ws:// or wss:// for WebSocket dialing
  gateway_url: "http://localhost:18800"
  
  # Origin header to inject
  origin: "https://gateway.local"
  
  # Shutdown settings
  drain_timeout: "30s"       # wait for active connections to finish on SIGTERM/SIGINT

  # WebSocket settings
  max_message_size: 1048576  # 1MB max WebSocket message size
  ping_interval: "30s"       # send ping frames to detect dead peers
  pong_timeout: "10s"        # close connection if pong not received within this window
  write_timeout: "10s"       # deadline for writing a single message
  dial_timeout: "10s"        # timeout for dialing upstream Gateway

  # TLS settings (optional, usually not needed with Tailscale)
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

security:
  # Only allow Tailscale IPs (IPv4: 100.64.0.0/10, IPv6: fd7a:115c:a1e0::/48)
  tailscale_only: true
  
  # Optional auth token
  # Clients should provide via Authorization: Bearer <token> header (preferred)
  # or ?token=xxx query parameter (fallback for development/testing)
  auth_token: ""
  
  # Rate limiting
  rate_limit:
    enabled: true
    connections_per_minute: 60
    messages_per_second: 100
  
  # Connection limits
  max_connections: 1000
  max_connections_per_ip: 10

logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json or text
  file: ""  # Empty = stdout, or path to log file
  # Log rotation (only when file is set)
  max_size_mb: 100     # max size in MB before rotation
  max_backups: 3       # number of old log files to retain
  max_age_days: 28     # max days to retain old log files
  compress: true       # gzip rotated log files

health:
  enabled: true
  endpoint: "/health"
  listen_address: "127.0.0.1:8081"  # Separate listener for health/metrics (accessible without Tailscale)
  
monitoring:
  metrics_enabled: false
  metrics_endpoint: "/metrics"  # Served on health listener (127.0.0.1:8081), not proxy listener
```

### 3. Core Components

#### 3.1 Proxy Handler

**Responsibilities:**
- Accept incoming WebSocket connections from ClawReach clients
- Validate client IP is in Tailscale range
- Dial upstream Gateway with proper Origin header
- Bidirectional message forwarding
- Handle disconnections and errors gracefully

**Key logic:**
```go
func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. Validate Tailscale IP
    if !isTailscaleIP(r.RemoteAddr) {
        http.Error(w, "Unauthorized", 403)
        return
    }

    // 2. Optional auth token check (header first, query param fallback)
    if h.config.AuthToken != "" {
        token := extractBearerToken(r.Header.Get("Authorization"))
        if token == "" {
            token = r.URL.Query().Get("token")
        }
        if !tokenMatch(token, h.config.AuthToken) {
            http.Error(w, "Forbidden", 403)
            return
        }
    }

    // 3. Rate limit check (strip port — RemoteAddr is "ip:port")
    clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
    if !h.rateLimiter.Allow(clientIP) {
        http.Error(w, "Too Many Requests", 429)
        return
    }

    // 4. Connection limits
    if h.proxy.ConnectionCount() >= h.config.MaxConnections {
        http.Error(w, "Service Unavailable", 503)
        return
    }
    if h.proxy.ConnectionCountForIP(clientIP) >= h.config.MaxConnectionsPerIP {
        http.Error(w, "Too Many Requests", 429)
        return
    }

    // 5. Accept client WebSocket connection
    // Forward subprotocols from client request to Gateway
    subprotocols := r.Header.Values("Sec-WebSocket-Protocol")
    clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        Subprotocols: subprotocols,
    })
    if err != nil {
        log.Error("Failed to accept client WebSocket", err)
        return
    }
    clientConn.SetReadLimit(h.config.MaxMessageSize) // default 1MB

    // 6. Dial Gateway with Origin header and matching subprotocols
    dialCtx, dialCancel := context.WithTimeout(r.Context(), h.config.DialTimeout)
    defer dialCancel()

    gatewayURL := httpToWS(h.config.GatewayURL) // http→ws, https→wss
    gatewayConn, _, err := websocket.Dial(dialCtx, gatewayURL, &websocket.DialOptions{
        HTTPHeader:   http.Header{"Origin": {h.config.Origin}},
        Subprotocols: subprotocols,
    })
    if err != nil {
        log.Error("Failed to dial gateway", err)
        clientConn.Close(websocket.StatusBadGateway, "gateway unreachable")
        return
    }
    gatewayConn.SetReadLimit(h.config.MaxMessageSize)

    // 7. Bidirectional forwarding with coordinated shutdown
    // When either direction finishes, cancel context to tear down the other side.
    // context.CancelFunc is safe to call multiple times.
    proxyCtx, proxyCancel := context.WithCancel(context.Background())
    var wg sync.WaitGroup
    wg.Add(2)
    go func() {
        defer wg.Done()
        defer proxyCancel()
        h.forwardMessages(proxyCtx, clientConn, gatewayConn, "client→gateway")
    }()
    go func() {
        defer wg.Done()
        defer proxyCancel()
        h.forwardMessages(proxyCtx, gatewayConn, clientConn, "gateway→client")
    }()

    // Cleanup: wait for both to finish, then close connections
    h.proxy.IncrementConnections()
    go func() {
        wg.Wait()
        clientConn.CloseNow()
        gatewayConn.CloseNow()
        h.proxy.DecrementConnections()
    }()
}

// forwardMessages reads from src and writes to dst until the context is
// cancelled or either side closes. This is the core proxy loop.
// direction is "client→gateway" or "gateway→client" for logging.
func (h *ProxyHandler) forwardMessages(ctx context.Context, src, dst *websocket.Conn, direction string) {
    for {
        msgType, reader, err := src.Reader(ctx)
        if err != nil {
            log.Debug("forward stopped", "direction", direction, "reason", err)
            return
        }

        writeCtx, writeCancel := context.WithTimeout(ctx, h.config.WriteTimeout)
        writer, err := dst.Writer(writeCtx, msgType)
        if err != nil {
            writeCancel()
            log.Debug("write failed", "direction", direction, "reason", err)
            return
        }

        if _, err := io.Copy(writer, reader); err != nil {
            writeCancel()
            log.Debug("copy failed", "direction", direction, "reason", err)
            return
        }
        if err := writer.Close(); err != nil {
            writeCancel()
            log.Debug("flush failed", "direction", direction, "reason", err)
            return
        }
        writeCancel()
    }
}

// extractBearerToken parses "Bearer <token>" from the Authorization header.
func extractBearerToken(authHeader string) string {
    const prefix = "Bearer "
    if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
        return authHeader[len(prefix):]
    }
    return ""
}

// tokenMatch uses constant-time comparison to prevent timing attacks.
func tokenMatch(provided, expected string) bool {
    return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
```

#### 3.2 Tailscale IP Validation

```go
// Package-level vars — parsed once at init, not per-request
var (
    tailscaleIPv4 = mustParseCIDR("100.64.0.0/10")   // Tailscale CGNAT range
    tailscaleIPv6 = mustParseCIDR("fd7a:115c:a1e0::/48") // Tailscale ULA range
)

func mustParseCIDR(s string) *net.IPNet {
    _, n, err := net.ParseCIDR(s)
    if err != nil {
        panic(err)
    }
    return n
}

func isTailscaleIP(addr string) bool {
    host, _, err := net.SplitHostPort(addr)
    if err != nil {
        return false
    }

    ip := net.ParseIP(host)
    if ip == nil {
        return false
    }

    return tailscaleIPv4.Contains(ip) || tailscaleIPv6.Contains(ip)
}
```

#### 3.3 Rate Limiter

Use `golang.org/x/time/rate` for token bucket algorithm with automatic cleanup of stale entries to prevent memory leaks:

```go
type ipLimiter struct {
    limiter  *rate.Limiter
    lastSeen time.Time
}

type RateLimiter struct {
    limiters map[string]*ipLimiter
    mu       sync.Mutex
    rate     rate.Limit
    burst    int
    ttl      time.Duration // evict entries not seen within this window
}

func NewRateLimiter(r rate.Limit, burst int) *RateLimiter {
    rl := &RateLimiter{
        limiters: make(map[string]*ipLimiter),
        rate:     r,
        burst:    burst,
        ttl:      10 * time.Minute,
    }
    go rl.cleanup() // background goroutine to evict stale entries
    return rl
}

func (rl *RateLimiter) Allow(ip string) bool {
    rl.mu.Lock()
    entry, exists := rl.limiters[ip]
    if !exists {
        entry = &ipLimiter{limiter: rate.NewLimiter(rl.rate, rl.burst)}
        rl.limiters[ip] = entry
    }
    entry.lastSeen = time.Now()
    rl.mu.Unlock()

    return entry.limiter.Allow()
}

func (rl *RateLimiter) cleanup() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        rl.mu.Lock()
        for ip, entry := range rl.limiters {
            if time.Since(entry.lastSeen) > rl.ttl {
                delete(rl.limiters, ip)
            }
        }
        rl.mu.Unlock()
    }
}
```

#### 3.4 Health Check

```go
type HealthResponse struct {
    Status            string         `json:"status"`
    Uptime            string         `json:"uptime"`
    ActiveConnections int            `json:"active_connections"`
    GatewayReachable  bool           `json:"gateway_reachable"`
    Version           string         `json:"version"`
    Timestamp         time.Time      `json:"timestamp"`
    Details           *HealthDetails `json:"details,omitempty"`
}

type HealthDetails struct {
    TotalConnections int64   `json:"total_connections"`
    TotalMessages    int64   `json:"total_messages"`
    ErrorsLastHour   int64   `json:"errors_last_hour"`
    MemoryMB         float64 `json:"memory_mb"`
}

// checkGateway verifies the upstream Gateway is reachable.
// Uses a plain HTTP request (not WebSocket dial) to avoid creating real
// connections and polluting Gateway logs on every health poll.
func (h *HealthHandler) checkGateway() bool {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.config.GatewayURL, nil)
    if err != nil {
        return false
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return false
    }
    resp.Body.Close()
    return true // any response (even 4xx) means Gateway is alive
}

// Health listener runs on 127.0.0.1:8081 (separate from proxy listener).
// This allows local monitoring tools (systemd, Prometheus, Nagios) to check
// health without being on the Tailscale network.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    gatewayOK := h.checkGateway()

    status := "ok"
    httpCode := http.StatusOK
    if !gatewayOK {
        status = "degraded"
        httpCode = http.StatusServiceUnavailable
    }

    resp := HealthResponse{
        Status:            status,
        Uptime:            time.Since(h.startTime).String(),
        ActiveConnections: h.proxy.ConnectionCount(),
        GatewayReachable:  gatewayOK,
        Version:           Version,
        Timestamp:         time.Now(),
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(httpCode)
    json.NewEncoder(w).Encode(resp)
}
```

### 4. Installation Experience

#### 4.1 One-Line Install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/master/scripts/install.sh | bash
```

**Install script does:**
1. Detect OS and architecture
2. Download appropriate binary from GitHub releases
3. Verify checksum
4. Install to `/usr/local/bin/clawreachbridge`
5. Create config directory `/etc/clawreachbridge/`
6. Run interactive setup wizard (or use `--non-interactive` flag)
7. Install systemd service (Linux) or launchd (macOS)
8. Start and enable service

#### 4.2 Interactive Setup Wizard

```bash
$ clawreachbridge setup

Welcome to ClawReach Bridge Setup
==================================

Detecting Tailscale...
✓ Tailscale is running (IP: 100.101.102.103)

OpenClaw Gateway URL [http://localhost:18800]: 
✓ Gateway reachable at http://localhost:18800

Bridge listen address [100.101.102.103:8080]: 
✓ Port 8080 is available

Enable authentication token? [y/N]: n

Configuration saved to /etc/clawreachbridge/config.yaml

Install systemd service? [Y/n]: y
✓ Service installed and started

ClawReach Bridge is ready!

Connect ClawReach clients to: ws://100.101.102.103:8080

Test connection:
  curl http://127.0.0.1:8081/health
```

#### 4.3 systemd Service File

```ini
[Unit]
Description=ClawReach Bridge - Secure WebSocket Proxy
Documentation=https://github.com/cortexuvula/clawreachbridge
After=network-online.target tailscaled.service
Wants=network-online.target
Requires=tailscaled.service

[Service]
Type=notify
User=clawreachbridge
Group=clawreachbridge
ExecStart=/usr/local/bin/clawreachbridge start --config /etc/clawreachbridge/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
WatchdogSec=30s

# Security hardening
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true
ReadOnlyPaths=/etc/clawreachbridge
LogsDirectory=clawreachbridge
StateDirectory=clawreachbridge
LimitNOFILE=65535

# Memory safety net: ~15MB base + ~20KB/connection × 1000 max = ~35MB typical
# Set headroom for message buffering spikes (max_message_size × active conns)
MemoryMax=128M

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=clawreachbridge

[Install]
WantedBy=multi-user.target
```

#### 4.4 Manual Installation

For users who prefer manual setup:

```bash
# Download binary
wget https://github.com/cortexuvula/clawreachbridge/releases/latest/download/clawreachbridge-linux-amd64
chmod +x clawreachbridge-linux-amd64
sudo mv clawreachbridge-linux-amd64 /usr/local/bin/clawreachbridge

# Create config
sudo mkdir -p /etc/clawreachbridge
sudo cp configs/config.example.yaml /etc/clawreachbridge/config.yaml
sudo nano /etc/clawreachbridge/config.yaml

# Install systemd service
sudo cp systemd/clawreachbridge.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable clawreachbridge
sudo systemctl start clawreachbridge

# Check status
sudo systemctl status clawreachbridge
```

### 5. Security Considerations

#### 5.1 Threat Model

**Assumptions:**
- Tailscale mesh is trusted (devices are authenticated, traffic encrypted)
- OpenClaw Gateway on localhost is trusted
- Bridge runs on same machine as Gateway (or trusted local network)

**Threats mitigated:**
- **Unauthorized access**: Tailscale-only binding prevents open internet access
- **Origin spoofing**: Bridge injects proper Origin, Gateway accepts it
- **DoS attacks**: Rate limiting per IP prevents abuse
- **Token leakage**: Auth token transmitted via header (not logged by default); query param fallback available for development only. Token can be rotated at runtime via SIGHUP config reload without dropping connections

**Threats NOT mitigated:**
- **Compromised Tailscale device**: If attacker compromises a device on the Tailscale network, they can access the bridge. Mitigation: Use Tailscale ACLs to restrict which devices can reach bridge.
- **Malicious Gateway**: If Gateway is compromised, bridge cannot protect. Mitigation: Harden Gateway host.

#### 5.2 Hardening Recommendations

1. **Tailscale ACLs**: Restrict bridge access to specific devices:
   ```json
   {
     "acls": [
       {
         "action": "accept",
         "src": ["tag:mobile"],
         "dst": ["tag:bridge:8080"]
       }
     ]
   }
   ```

2. **Run as non-root**: Use systemd `User=clawreachbridge` directive

3. **Minimal capabilities**: Only `CAP_NET_BIND_SERVICE` if binding to port <1024

4. **Read-only config**: `chmod 600 /etc/clawreachbridge/config.yaml`

5. **Audit logging**: Enable JSON logs, forward to syslog or log aggregator

6. **Automatic updates**: Use systemd timer to check for new releases

7. **Monitor /health**: Alert if `gateway_reachable: false`

### 6. Testing Strategy

#### 6.1 Unit Tests
- Config loading and validation
- Tailscale IP detection (test vectors for CGNAT range)
- Rate limiter behavior
- Health check response format

#### 6.2 Integration Tests
- Full proxy flow: ClawReach → Bridge → Gateway
- Reconnection on Gateway restart
- Rate limiting enforcement
- Authentication token validation

#### 6.3 Load Tests
- 100 concurrent connections
- 1000 messages/second throughput
- Memory usage under load
- Connection leak detection

#### 6.4 Security Tests
- Non-Tailscale IP rejection
- Invalid auth token rejection
- Rate limit bypass attempts
- Header injection attempts

### 7. Deployment Scenarios

#### 7.1 Scenario A: Single User, Personal Server
- Gateway + Bridge on same machine (e.g., home server)
- Tailscale mesh with laptop + phone + server
- Bridge listens on Tailscale IP only
- No auth token needed (Tailscale is sufficient)

**Config:**
```yaml
bridge:
  listen_address: "100.x.x.x:8080"
  gateway_url: "http://localhost:18800"
security:
  tailscale_only: true
  auth_token: ""
```

#### 7.2 Scenario B: Multi-User, Shared Server
- Gateway + Bridge on VPS
- Multiple users' devices on Tailscale
- Auth token per user (optional, if you want per-user tracking)
- Rate limiting to prevent abuse

**Config:**
```yaml
bridge:
  listen_address: "100.x.x.x:8080"
  gateway_url: "http://localhost:18800"
security:
  tailscale_only: true
  auth_token: "secret-shared-with-users"
  rate_limit:
    enabled: true
    connections_per_minute: 30
```

#### 7.3 Scenario C: High Availability
- Gateway on machine A
- Bridge on machine B (in same Tailscale network)
- Multiple bridge instances with load balancer (future enhancement)

**Config:**
```yaml
bridge:
  listen_address: "100.y.y.y:8080"
  gateway_url: "http://100.x.x.x:18800"  # Gateway's Tailscale IP
```

### 8. CLI Commands

```bash
# Start bridge (default config)
clawreachbridge start

# Start with custom config
clawreachbridge start --config /path/to/config.yaml

# Interactive setup wizard
clawreachbridge setup

# Validate config without starting
clawreachbridge validate --config /path/to/config.yaml

# Show version and build info
clawreachbridge version

# Health check (exit 0 if healthy, 1 if not)
clawreachbridge health --url http://127.0.0.1:8081/health

# Generate systemd service file
clawreachbridge systemd --print > clawreachbridge.service

# Run in foreground with verbose logging
clawreachbridge start --verbose --foreground
```

### 9. Metrics & Monitoring

#### 9.1 Prometheus Metrics (optional)

```
# HELP clawreachbridge_connections_total Total connections handled
# TYPE clawreachbridge_connections_total counter
clawreachbridge_connections_total 1234

# HELP clawreachbridge_active_connections Current active connections
# TYPE clawreachbridge_active_connections gauge
clawreachbridge_active_connections 42

# HELP clawreachbridge_messages_total Total messages proxied
# TYPE clawreachbridge_messages_total counter
clawreachbridge_messages_total{direction="upstream"} 5678
clawreachbridge_messages_total{direction="downstream"} 5670

# HELP clawreachbridge_errors_total Total errors
# TYPE clawreachbridge_errors_total counter
clawreachbridge_errors_total{type="dial_failure"} 3

# HELP clawreachbridge_gateway_reachable Gateway reachability (1=up, 0=down)
# TYPE clawreachbridge_gateway_reachable gauge
clawreachbridge_gateway_reachable 1
```

#### 9.2 Health Check Response

```json
{
  "status": "ok",
  "uptime": "24h15m30s",
  "active_connections": 5,
  "gateway_reachable": true,
  "version": "1.0.0",
  "timestamp": "2026-02-07T11:15:00Z",
  "details": {
    "total_connections": 1234,
    "total_messages": 11348,
    "errors_last_hour": 0,
    "memory_mb": 12.5
  }
}
```

### 10. Documentation Structure

#### 10.1 README.md
- Quick start guide
- Installation options
- Basic configuration
- Links to detailed docs

#### 10.2 INSTALLATION.md
- Detailed installation for Linux, macOS, Windows (if supported)
- systemd setup
- Docker image (future)
- Kubernetes deployment (future)

#### 10.3 CONFIGURATION.md
- Full config schema reference
- Environment variable overrides
- Examples for common scenarios

#### 10.4 SECURITY.md
- Threat model
- Hardening checklist
- Tailscale ACL examples
- Audit logging setup

#### 10.5 TROUBLESHOOTING.md
- Common errors and solutions
- Debug logging
- Connection test procedures
- Performance tuning

### 11. Release Process

#### 11.1 Versioning
- Semantic versioning: MAJOR.MINOR.PATCH
- Git tags: `v1.0.0`, `v1.0.1`, etc.

#### 11.2 Build Artifacts
Cross-compile for:
- Linux: amd64, arm64, armv7 (Raspberry Pi)
- macOS: amd64, arm64 (M1/M2)
- Windows: amd64 (future, if demand exists)

#### 11.3 GitHub Release
- Automated via GitHub Actions
- Binaries attached to release
- Checksums (SHA256) provided
- Changelog auto-generated from commits

#### 11.4 Update Mechanism
- Bridge checks GitHub API for latest release (optional, opt-in)
- Notify user if newer version available
- Future: Auto-update with `--auto-update` flag

### 12. Development Roadmap

#### Phase 1: MVP (v0.1.0) - Week 1
- [ ] Basic WebSocket proxy with subprotocol forwarding
- [ ] Config file loading with env var overrides (`CLAWREACH_` prefix)
- [ ] Tailscale IP validation (IPv4 + IPv6)
- [ ] Health check endpoint on separate listener (127.0.0.1:8081)
- [ ] WebSocket keepalive (ping/pong) and message size limits
- [ ] Coordinated connection shutdown via context.Context
- [ ] Unit tests

#### Phase 2: Security & Installation (v0.2.0) - Week 2
- [ ] Rate limiting with automatic stale-entry cleanup
- [ ] Auth token support (header primary, query param fallback, constant-time compare)
- [ ] Structured logging with log rotation (lumberjack)
- [ ] SIGHUP config reload (rate limits, auth tokens, log level)
- [ ] Install script
- [ ] systemd service file (Type=notify, security hardening, ExecReload)
- [ ] Documentation

#### Phase 3: Production-Ready (v1.0.0) - Week 3
- [ ] Integration tests
- [ ] Load testing
- [ ] CLI commands (setup, validate, etc.)
- [ ] Error handling polish
- [ ] Security audit
- [ ] Release automation

#### Phase 4: Enhancements (v1.x) - Future
- [ ] Prometheus metrics
- [ ] Connection pooling
- [ ] Docker image
- [ ] Kubernetes manifests
- [ ] Web UI for monitoring (optional)
- [ ] Multiple upstream Gateways (HA)

### 13. Success Criteria

#### For v1.0.0 Release:
1. **Functionality**: ClawReach clients can connect through bridge to Gateway without errors
2. **Security**: Only Tailscale IPs accepted, Origin header injected correctly
3. **Performance**: <10ms latency overhead, handles 100 concurrent connections
4. **Reliability**: Runs for 7 days without crashes or memory leaks
5. **Usability**: One-line install works on Ubuntu, Debian, Fedora, macOS
6. **Documentation**: User can install and configure in <5 minutes from README
7. **Testing**: >80% code coverage, all integration tests passing

### 14. Dependencies

#### Go Modules:
- `github.com/coder/websocket` - WebSocket library (actively maintained, idiomatic net/http patterns, native context.Context support)
- `gopkg.in/yaml.v3` - YAML parsing
- `golang.org/x/time/rate` - Rate limiting
- `github.com/sirupsen/logrus` or `go.uber.org/zap` - Structured logging
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation (when logging to file)
- `github.com/spf13/cobra` - CLI framework (optional, for commands)
- `github.com/spf13/viper` - Config management (optional)

#### External:
- Tailscale (must be installed and running)
- OpenClaw Gateway (must be running)
- systemd (Linux) or launchd (macOS) for service management

### 15. FAQ

**Q: Why not just fix OpenClaw's origin check?**  
A: The PR may not be accepted due to security concerns. Bridge gives us control and doesn't depend on upstream.

**Q: Does this make my setup less secure?**  
A: No. Tailscale already provides strong security (mutual TLS, device auth). Bridge adds origin headers that Gateway requires, without weakening the security model.

**Q: Can I run this on the same machine as Gateway?**  
A: Yes, that's the primary use case. Bridge listens on Tailscale IP, Gateway on localhost. All traffic stays on one machine.

**Q: What if Tailscale goes down?**  
A: Remote ClawReach connections stop working since the bridge is bound to the Tailscale IP. There is no LAN fallback — connecting ClawReach directly to Gateway over LAN hits the same Origin header restriction that necessitates the bridge. Tailscale is required for bridge connectivity.

**Q: How much overhead does this add?**  
A: Minimal. Go's goroutines are very efficient. Expect <10ms latency and <20MB memory for typical loads.

**Q: Can multiple users share one bridge?**  
A: Yes. Rate limiting and connection limits protect against abuse. For per-user tracking, use auth tokens.

**Q: Is this safe to run on a public VPS?**  
A: Yes, as long as the bridge only listens on the Tailscale IP (not 0.0.0.0). Tailscale traffic is encrypted end-to-end.

**Q: Can I use this without Tailscale?**  
A: Not recommended. The security model relies on Tailscale's authentication. Without it, you'd need to implement your own auth (TLS client certs, tokens, etc.).

### 16. Contributing Guidelines

#### Code Style:
- Follow `gofmt` and `golint`
- Use meaningful variable names
- Document exported functions
- Write tests for new features

#### Pull Request Process:
1. Fork the repo
2. Create feature branch (`git checkout -b feature/my-feature`)
3. Write tests
4. Ensure `go test ./...` passes
5. Run `go fmt ./...`
6. Update documentation if needed
7. Submit PR with clear description

#### Issue Reporting:
- Use issue templates (bug report, feature request)
- Include logs, config (sanitized), and version info
- Describe steps to reproduce

---

## Conclusion

This implementation plan provides a comprehensive blueprint for building ClawReach Bridge. The design prioritizes:

1. **Security**: Tailscale-only access, rate limiting, audit logging
2. **Ease of use**: One-line install, interactive setup, clear docs
3. **Reliability**: Graceful error handling, health checks, systemd integration
4. **Performance**: Efficient Go implementation, <10ms latency overhead
5. **Maintainability**: Clean code structure, comprehensive tests, good documentation

The MVP can be built in 1-2 weeks by an experienced Go developer. Claude Code Opus 4.6 should be able to implement this with the level of detail provided.

**Next steps:**
1. Review this plan with Andre
2. Create GitHub issues for each phase
3. Begin Phase 1 implementation
4. Iterate based on testing and feedback

**Estimated timeline:**
- v0.1.0 (MVP): 1 week
- v0.2.0 (Security + Install): 1 week  
- v1.0.0 (Production-ready): 1 week
- Total: ~3 weeks to production-ready release
