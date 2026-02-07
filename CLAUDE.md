# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ClawReach Bridge is a secure WebSocket proxy written in Go that enables ClawReach mobile/web clients to connect to OpenClaw Gateway over Tailscale. It solves the WebSocket origin check issue (GitHub #9358, PR #10695, CVE-2026-25253) by acting as a trusted intermediary that injects proper Origin headers while relying on Tailscale's encrypted mesh for authentication.

**Status:** Under active development. The implementation plan is complete; source code implementation follows the phased roadmap in `IMPLEMENTATION_PLAN.md`.

## Architecture

```
ClawReach Client (mobile/web)
        ↓ (Tailscale, encrypted)
ClawReach Bridge (this project)
        ↓ (localhost, proper Origin headers)
OpenClaw Gateway (unmodified)
```

Tailscale provides the security boundary. The bridge translates Tailscale-authenticated connections into localhost connections that Gateway trusts. Proxy binds to Tailscale IP only (IPv4: `100.64.0.0/10`, IPv6: `fd7a:115c:a1e0::/48`). Health endpoint runs on a separate listener (`127.0.0.1:8081`) so local monitoring tools work without Tailscale access.

## Build & Development Commands

```bash
# Build
go build -o clawreachbridge ./cmd/clawreachbridge

# Run tests
go test ./...

# Format code
go fmt ./...

# Run a single test
go test -run TestName ./internal/proxy/

# Run with race detector
go test -race ./...

# Cross-compile (example: Linux ARM64)
GOOS=linux GOARCH=arm64 go build -o clawreachbridge-linux-arm64 ./cmd/clawreachbridge

# Run in verbose/foreground mode
./clawreachbridge start --verbose --foreground

# Validate config
./clawreachbridge validate --config /path/to/config.yaml
```

## Code Structure

```
cmd/clawreachbridge/main.go    — Entry point, CLI parsing
internal/
  config/                      — YAML config loading, validation, env var overrides
  proxy/                       — Core WebSocket proxy (handler, connection mgmt, bidirectional forwarding)
  security/                    — Tailscale IP validation, per-IP rate limiting (token bucket), optional auth tokens
  health/                      — /health JSON endpoint (uptime, active connections, gateway reachability)
  logging/                     — Structured JSON logging setup
test/
  integration/                 — Integration tests (build tag: integration)
  loadtest/                    — WebSocket load testing tools
scripts/                       — install.sh, uninstall.sh, build.sh
systemd/                       — clawreachbridge.service unit file
configs/                       — config.example.yaml
```

## Key Design Decisions

- **Go** chosen for single static binary, cross-compilation, goroutine concurrency, and `coder/websocket` with native `context.Context` support
- **Tailscale-only security model**: clients must be on the Tailscale network; no public internet exposure
- **Zero Gateway changes**: works with unmodified OpenClaw Gateway by injecting Origin headers
- **Config format**: YAML with env var overrides (12-factor pattern). Config path: `/etc/clawreachbridge/config.yaml`
- **Service management**: systemd on Linux, launchd on macOS

## Dependencies

- `github.com/coder/websocket` — WebSocket library (actively maintained, context-aware)
- `gopkg.in/yaml.v3` — YAML parsing
- `golang.org/x/time/rate` — Token bucket rate limiting
- `gopkg.in/natefinch/lumberjack.v2` — Log rotation
- Logging: `logrus` or `zap` (TBD)
- CLI: `cobra`/`viper` (optional)

## Contributing

- Follow `gofmt` and `golint`
- Write tests for new features (`go test ./...` must pass)
- Feature branches: `feature/my-feature`
- Config files with secrets are gitignored — use `configs/config.example.yaml` as reference
