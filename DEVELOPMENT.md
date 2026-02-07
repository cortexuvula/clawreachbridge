# Development Guide

This guide is for developers working on ClawReach Bridge.

## Prerequisites

### Required
- **Go 1.21+**: [Download](https://go.dev/dl/)
- **Git**: For version control
- **Tailscale**: Running and connected (for testing)
- **OpenClaw Gateway**: Running locally (for integration tests)

### Optional
- **wscat**: WebSocket command-line client (`npm install -g wscat`)
- **jq**: JSON processor for parsing responses (`apt install jq` or `brew install jq`)
- **golangci-lint**: Linter for code quality checks
- **hey**: HTTP load testing tool

## Quick Start

```bash
# Clone the repository
git clone https://github.com/cortexuvula/clawreachbridge.git
cd clawreachbridge

# Install dependencies
go mod download

# Build
go build -o clawreachbridge ./cmd/clawreachbridge

# Run (requires config file)
./clawreachbridge start --config configs/config.example.yaml
```

## Project Structure

```
clawreachbridge/
├── cmd/clawreachbridge/    # Main entry point
├── internal/               # Private application code
│   ├── config/            # Configuration loading/validation
│   ├── proxy/             # WebSocket proxy logic
│   ├── security/          # Auth, rate limiting, Tailscale validation
│   ├── health/            # Health check endpoint
│   └── logging/           # Structured logging
├── configs/               # Example configuration files
├── scripts/               # Build/install scripts
├── systemd/               # Service files
└── docs/                  # Documentation
```

## Building

### Development Build

```bash
# Build with debug symbols
go build -o clawreachbridge ./cmd/clawreachbridge

# Build with race detector (slower, detects concurrency bugs)
go build -race -o clawreachbridge ./cmd/clawreachbridge
```

### Production Build

```bash
# Optimized build with version info
VERSION=$(git describe --tags --always --dirty)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(git rev-parse HEAD)

go build \
  -ldflags="-s -w -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME -X main.GitCommit=$GIT_COMMIT" \
  -o clawreachbridge \
  ./cmd/clawreachbridge

# Verify binary size (should be ~10-15MB)
ls -lh clawreachbridge
```

### Cross-Compilation

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o clawreachbridge-linux-amd64 ./cmd/clawreachbridge

# Linux ARM64 (Raspberry Pi 4, AWS Graviton)
GOOS=linux GOARCH=arm64 go build -o clawreachbridge-linux-arm64 ./cmd/clawreachbridge

# Linux ARM v7 (Raspberry Pi 3, older)
GOOS=linux GOARCH=arm GOARM=7 go build -o clawreachbridge-linux-armv7 ./cmd/clawreachbridge

# macOS AMD64 (Intel)
GOOS=darwin GOARCH=amd64 go build -o clawreachbridge-darwin-amd64 ./cmd/clawreachbridge

# macOS ARM64 (M1/M2/M3)
GOOS=darwin GOARCH=arm64 go build -o clawreachbridge-darwin-arm64 ./cmd/clawreachbridge

# Windows AMD64 (future)
GOOS=windows GOARCH=amd64 go build -o clawreachbridge-windows-amd64.exe ./cmd/clawreachbridge
```

Use `scripts/build.sh` to build all platforms at once:

```bash
./scripts/build.sh
ls -lh dist/
```

## Configuration

### Example Config for Development

Create `dev-config.yaml`:

```yaml
bridge:
  listen_address: "100.101.102.103:8080"  # Replace with your Tailscale IP
  gateway_url: "http://localhost:18800"
  origin: "https://gateway.local"
  drain_timeout: "5s"  # Shorter for faster restarts in dev

security:
  tailscale_only: true
  auth_token: "dev-test-token-replace-in-production"
  rate_limit:
    enabled: false  # Disable for easier testing
  max_connections: 100
  max_connections_per_ip: 10

logging:
  level: "debug"  # Verbose logging for development
  format: "text"  # Easier to read than JSON
  file: ""  # Log to stdout

health:
  enabled: true
  listen_address: "127.0.0.1:8081"
  endpoint: "/health"
```

### Get Your Tailscale IP

```bash
# Show all Tailscale IPs
tailscale ip

# IPv4 only
tailscale ip -4

# IPv6 only
tailscale ip -6
```

## Running

### Foreground (with logs)

```bash
./clawreachbridge start --config dev-config.yaml --foreground --verbose
```

### Background

```bash
# Start
./clawreachbridge start --config dev-config.yaml &

# Check logs
tail -f /var/log/clawreachbridge/clawreachbridge.log

# Stop
pkill clawreachbridge
```

### systemd (Linux)

```bash
# Install service
sudo cp systemd/clawreachbridge.service /etc/systemd/system/
sudo systemctl daemon-reload

# Start
sudo systemctl start clawreachbridge

# View logs
sudo journalctl -u clawreachbridge -f

# Stop
sudo systemctl stop clawreachbridge

# Restart (graceful shutdown)
sudo systemctl restart clawreachbridge

# Reload config (SIGHUP)
sudo systemctl reload clawreachbridge
```

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
open coverage.html

# Run specific package
go test ./internal/security/

# Run specific test
go test -run TestTailscaleIPValidation ./internal/security/

# Verbose output
go test -v ./...

# Run with race detector
go test -race ./...
```

### Integration Tests

```bash
# Requires OpenClaw Gateway running on localhost:18800
go test -tags=integration ./test/integration/

# Or manually:
# Terminal 1: Start Gateway
openclaw gateway

# Terminal 2: Start Bridge
./clawreachbridge start --config dev-config.yaml

# Terminal 3: Run tests
go test -tags=integration ./test/integration/
```

### Manual Testing

#### 1. Health Check

```bash
# Check health endpoint
curl http://127.0.0.1:8081/health | jq

# Expected output:
# {
#   "status": "ok",
#   "uptime": "5m23s",
#   "active_connections": 0,
#   "gateway_reachable": true,
#   "version": "v0.1.0-dev",
#   "timestamp": "2026-02-07T12:00:00Z"
# }

# Check if gateway is reachable
curl -s http://127.0.0.1:8081/health | jq -r .gateway_reachable
```

#### 2. WebSocket Connection (wscat)

```bash
# Install wscat if not already installed
npm install -g wscat

# Connect without auth token
wscat -c ws://$(tailscale ip -4):8080

# Connect with auth token (query param)
wscat -c "ws://$(tailscale ip -4):8080?token=dev-test-token-replace-in-production"

# Connect with auth token (header - requires wscat v5+)
wscat -c ws://$(tailscale ip -4):8080 -H "Authorization: Bearer dev-test-token-replace-in-production"

# Once connected, send a message:
> {"type": "ping"}

# You should see Gateway's response forwarded back
```

#### 3. WebSocket Connection (curl)

```bash
# Upgrade to WebSocket (requires curl 7.86+ with --http1.1 flag)
curl --http1.1 \
  --include \
  --no-buffer \
  --header "Connection: Upgrade" \
  --header "Upgrade: websocket" \
  --header "Sec-WebSocket-Version: 13" \
  --header "Sec-WebSocket-Key: SGVsbG8sIHdvcmxkIQ==" \
  --header "Authorization: Bearer dev-test-token-replace-in-production" \
  http://$(tailscale ip -4):8080/

# Should see HTTP 101 Switching Protocols
```

#### 4. Test Tailscale IP Validation

```bash
# From Tailscale device (should succeed)
wscat -c ws://$(tailscale ip -4):8080

# From non-Tailscale IP (should fail with 403)
# Connect from a different machine not on your Tailscale network
wscat -c ws://your-public-ip:8080
# Error: Unexpected server response: 403
```

#### 5. Test Rate Limiting

```bash
# Enable rate limiting in config first
# rate_limit.connections_per_minute: 10

# Rapid connections (should hit rate limit)
for i in {1..20}; do
  wscat -c ws://$(tailscale ip -4):8080 &
done

# Check logs for rate limit messages
tail -f clawreachbridge.log | grep "rate limit"
```

#### 6. Test Config Reload (SIGHUP)

```bash
# Start bridge
./clawreachbridge start --config dev-config.yaml &
PID=$!

# Edit config (change log level, auth token, etc.)
nano dev-config.yaml

# Reload config without restarting
kill -HUP $PID

# Check logs for reload message
tail clawreachbridge.log | grep "config reloaded"
```

### Load Testing

#### Simple Load Test (hey)

```bash
# Install hey
go install github.com/rakyll/hey@latest

# HTTP load test (health endpoint)
hey -n 10000 -c 100 http://127.0.0.1:8081/health

# WebSocket load test (more complex, use custom tool)
# See test/loadtest/ws-loadtest.go
go run test/loadtest/ws-loadtest.go -url ws://$(tailscale ip -4):8080 -conns 100 -duration 60s
```

#### Memory Profiling

```bash
# Start with pprof enabled (add to main.go)
# import _ "net/http/pprof"
# go http.ListenAndServe("localhost:6060", nil)

# Run for a while under load
hey -n 100000 -c 100 http://127.0.0.1:8081/health

# Capture heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof

# Analyze
go tool pprof -http=:8080 heap.prof
```

## Debugging

### Enable Debug Logging

```yaml
# dev-config.yaml
logging:
  level: "debug"  # Prints all connection events
  format: "text"  # Human-readable (use "json" for structured logs)
```

### Common Issues

#### 1. "Tailscale not running"

```bash
# Check Tailscale status
tailscale status

# If down, start it
sudo systemctl start tailscaled  # Linux
# or
brew services start tailscale    # macOS
```

#### 2. "Gateway unreachable"

```bash
# Check if Gateway is running
curl http://localhost:18800/health

# Check Gateway logs
tail -f ~/.openclaw/logs/gateway.log

# Verify Gateway URL in config
grep gateway_url dev-config.yaml
```

#### 3. "Port already in use"

```bash
# Check what's using port 8080
sudo lsof -i :8080

# Kill it
sudo kill -9 <PID>

# Or use a different port in config
```

#### 4. "Authorization header not forwarded"

Check that you're using `coder/websocket` v1.8+, which supports custom headers. Verify in `go.mod`:

```bash
grep coder/websocket go.mod
# Should show: github.com/coder/websocket v1.8.x
```

### Inspect WebSocket Frames

Use browser DevTools or Wireshark:

```bash
# Wireshark filter for WebSocket traffic on port 8080
tcp.port == 8080 and websocket

# Or use tshark (command-line Wireshark)
tshark -i any -f "tcp port 8080" -Y websocket
```

### Verbose Output

```bash
# Maximum verbosity
./clawreachbridge start --config dev-config.yaml --verbose --foreground 2>&1 | tee debug.log

# Or set in code (main.go)
log.SetLevel(log.DebugLevel)
```

## Code Quality

### Linting

```bash
# Install golangci-lint
# https://golangci-lint.run/usage/install/
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Run linter
golangci-lint run

# Auto-fix issues where possible
golangci-lint run --fix
```

### Formatting

```bash
# Format all code
go fmt ./...

# Check for formatting issues
gofmt -l .

# Use goimports (formats + manages imports)
go install golang.org/x/tools/cmd/goimports@latest
goimports -w .
```

### Vet (Static Analysis)

```bash
# Check for suspicious constructs
go vet ./...
```

## Benchmarking

```bash
# Run benchmarks
go test -bench=. ./internal/proxy/

# With memory allocation stats
go test -bench=. -benchmem ./internal/proxy/

# CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./internal/proxy/
go tool pprof cpu.prof
```

## Git Workflow

### Branch Naming

- `feature/xyz` - New features
- `fix/xyz` - Bug fixes
- `docs/xyz` - Documentation
- `refactor/xyz` - Code refactoring
- `test/xyz` - Test improvements

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add support for multiple upstream gateways
fix: prevent memory leak in rate limiter cleanup
docs: add troubleshooting guide for Tailscale issues
test: add integration tests for auth token validation
refactor: simplify WebSocket forwarding logic
```

### Before Committing

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Run tests
go test ./...

# Run tests with race detector
go test -race ./...

# Stage changes
git add .

# Commit
git commit -m "feat: add XYZ"

# Push
git push origin feature/xyz
```

## Release Process

### Versioning

Use semantic versioning: `vMAJOR.MINOR.PATCH`

- **MAJOR**: Breaking changes
- **MINOR**: New features (backward-compatible)
- **PATCH**: Bug fixes

### Create a Release

```bash
# Tag the commit
git tag -a v0.1.0 -m "Release v0.1.0: MVP"

# Push tag
git push origin v0.1.0

# GitHub Actions will automatically:
# 1. Build binaries for all platforms
# 2. Generate SHA256 checksums
# 3. Create GitHub release
# 4. Attach artifacts
```

### Manual Release Build

```bash
# Build all platforms
./scripts/build.sh

# Generate checksums
cd dist/
sha256sum * > checksums.txt

# Create release archive
tar -czf clawreachbridge-v0.1.0.tar.gz *
```

## Troubleshooting Development Issues

### Go Module Issues

```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download

# Verify dependencies
go mod verify

# Tidy up (remove unused, add missing)
go mod tidy
```

### Build Issues

```bash
# Clean build cache
go clean -cache

# Rebuild everything
go build -a ./cmd/clawreachbridge
```

### IDE Setup

#### VS Code

Install extensions:
- **Go** (golang.go)
- **Go Test Explorer** (ethan-reesor.vscode-go-test-adapter)

Settings (`.vscode/settings.json`):

```json
{
  "go.useLanguageServer": true,
  "go.lintOnSave": "workspace",
  "go.formatTool": "goimports",
  "go.testFlags": ["-v", "-race"],
  "go.coverOnSave": true
}
```

#### GoLand / IntelliJ IDEA

1. Open project
2. Configure Go SDK: File → Project Structure → SDKs
3. Enable Go Modules: Preferences → Go → Go Modules
4. Run configurations: Add "Go Build" configuration pointing to `cmd/clawreachbridge/main.go`

## Performance Tips

### Reduce Allocations

```go
// Bad: allocates new buffer every time
buf := make([]byte, 1024)

// Good: reuse buffer pool
var bufPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 1024)
    },
}
buf := bufPool.Get().([]byte)
defer bufPool.Put(buf)
```

### Avoid Copying Large Messages

Use `io.Copy` instead of reading entire message into memory:

```go
// Good (already in plan)
_, err := io.Copy(writer, reader)
```

### Profile Before Optimizing

```bash
# CPU profile
go test -cpuprofile=cpu.prof -bench=.
go tool pprof -http=:8080 cpu.prof

# Memory profile
go test -memprofile=mem.prof -bench=.
go tool pprof -http=:8080 mem.prof
```

## References

- [Go Documentation](https://go.dev/doc/)
- [coder/websocket Documentation](https://pkg.go.dev/github.com/coder/websocket)
- [Tailscale API](https://tailscale.com/kb/1080/cli)
- [systemd Service File Documentation](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [OpenClaw Documentation](https://docs.openclaw.ai)

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/cortexuvula/clawreachbridge/issues)
- **Discussions**: [GitHub Discussions](https://github.com/cortexuvula/clawreachbridge/discussions)
- **OpenClaw Discord**: [discord.com/invite/clawd](https://discord.com/invite/clawd)

---

*Happy hacking! 🦊*
