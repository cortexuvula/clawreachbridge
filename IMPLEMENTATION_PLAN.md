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
- **Strong WebSocket support**: `gorilla/websocket` is battle-tested
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
- Handle both secure (wss://) and insecure (ws://) upstream
- Automatic reconnection with exponential backoff
- Connection pooling to Gateway (optional, for multiple clients)

### 2. Security
- **Tailscale-only listening**: Bind to Tailscale interface IP only, never 0.0.0.0
- **Connection validation**: Verify client IP is in Tailscale network range (100.64.0.0/10)
- **Origin header injection**: Add `Origin: https://gateway.local` or configured value
- **Optional auth token**: Support `?token=xxx` query parameter for additional validation
- **TLS support**: Option to terminate TLS at bridge (though Tailscale already encrypts)
- **Rate limiting**: Per-IP connection and message rate limits (configurable)
- **Audit logging**: Log all connections with timestamps, source IPs, durations

### 3. Health & Monitoring
- **Health check endpoint**: `/health` returns JSON status (upstream reachable, active connections, uptime)
- **Metrics endpoint**: `/metrics` returns Prometheus-compatible metrics (optional)
- **Structured logging**: JSON logs with levels (DEBUG, INFO, WARN, ERROR)
- **Graceful shutdown**: Close connections cleanly on SIGTERM/SIGINT
- **Watchdog support**: systemd sd_notify for service monitoring

### 4. Configuration
- **Config file**: YAML or TOML format (prefer YAML for readability)
- **Environment variables**: Override config with env vars (12-factor app pattern)
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
├── docs/
│   │── INSTALLATION.md             # Installation guide
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
  
  # OpenClaw Gateway upstream
  gateway_url: "http://localhost:18800"
  
  # Origin header to inject
  origin: "https://gateway.local"
  
  # TLS settings (optional, usually not needed with Tailscale)
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

security:
  # Only allow Tailscale IPs (100.64.0.0/10)
  tailscale_only: true
  
  # Optional auth token (clients must provide ?token=xxx)
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

health:
  enabled: true
  endpoint: "/health"
  
monitoring:
  metrics_enabled: false
  metrics_endpoint: "/metrics"
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
    
    // 2. Optional auth token check
    if h.config.AuthToken != "" && r.URL.Query().Get("token") != h.config.AuthToken {
        http.Error(w, "Forbidden", 403)
        return
    }
    
    // 3. Rate limit check
    if !h.rateLimiter.Allow(r.RemoteAddr) {
        http.Error(w, "Too Many Requests", 429)
        return
    }
    
    // 4. Upgrade client connection
    clientConn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Error("Failed to upgrade client", err)
        return
    }
    
    // 5. Dial Gateway with Origin header
    header := http.Header{}
    header.Set("Origin", h.config.Origin)
    gatewayConn, _, err := websocket.DefaultDialer.Dial(h.config.GatewayURL, header)
    if err != nil {
        log.Error("Failed to dial gateway", err)
        clientConn.Close()
        return
    }
    
    // 6. Bidirectional forwarding
    go h.forwardMessages(clientConn, gatewayConn)
    go h.forwardMessages(gatewayConn, clientConn)
}
```

#### 3.2 Tailscale IP Validation

```go
func isTailscaleIP(addr string) bool {
    host, _, err := net.SplitHostPort(addr)
    if err != nil {
        return false
    }
    
    ip := net.ParseIP(host)
    if ip == nil {
        return false
    }
    
    // Tailscale uses 100.64.0.0/10 (CGNAT range)
    _, tailscaleNet, _ := net.ParseCIDR("100.64.0.0/10")
    return tailscaleNet.Contains(ip)
}
```

#### 3.3 Rate Limiter

Use `golang.org/x/time/rate` for token bucket algorithm:

```go
type RateLimiter struct {
    limiters map[string]*rate.Limiter
    mu       sync.RWMutex
    rate     rate.Limit
    burst    int
}

func (rl *RateLimiter) Allow(ip string) bool {
    rl.mu.Lock()
    limiter, exists := rl.limiters[ip]
    if !exists {
        limiter = rate.NewLimiter(rl.rate, rl.burst)
        rl.limiters[ip] = limiter
    }
    rl.mu.Unlock()
    
    return limiter.Allow()
}
```

#### 3.4 Health Check

```go
type HealthResponse struct {
    Status            string    `json:"status"`
    Uptime            string    `json:"uptime"`
    ActiveConnections int       `json:"active_connections"`
    GatewayReachable  bool      `json:"gateway_reachable"`
    Version           string    `json:"version"`
    Timestamp         time.Time `json:"timestamp"`
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Quick check: can we reach Gateway?
    gatewayOK := h.checkGateway()
    
    status := "ok"
    if !gatewayOK {
        status = "degraded"
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
    json.NewEncoder(w).Encode(resp)
}
```

### 4. Installation Experience

#### 4.1 One-Line Install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/main/scripts/install.sh | bash
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
  curl http://100.101.102.103:8080/health
```

#### 4.3 Manual Installation

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
- **Token leakage**: Optional auth token adds second factor (defense in depth)

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
clawreachbridge health --url http://localhost:8080/health

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
- [ ] Basic WebSocket proxy (no security)
- [ ] Config file loading
- [ ] Tailscale IP validation
- [ ] Health check endpoint
- [ ] Unit tests

#### Phase 2: Security & Installation (v0.2.0) - Week 2
- [ ] Rate limiting
- [ ] Auth token support
- [ ] Structured logging
- [ ] Install script
- [ ] systemd service file
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
- `github.com/gorilla/websocket` - WebSocket library
- `gopkg.in/yaml.v3` - YAML parsing
- `golang.org/x/time/rate` - Rate limiting
- `github.com/sirupsen/logrus` or `go.uber.org/zap` - Structured logging
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
A: ClawReach won't be able to connect remotely, but local connections (same LAN) could still work if you configure ClawReach to use the LAN IP. However, the primary design assumes Tailscale is the connectivity layer.

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
