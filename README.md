# ClawReach Bridge

**Secure WebSocket proxy for ClawReach ↔ OpenClaw Gateway over Tailscale**

## Problem

ClawReach mobile/web clients cannot connect directly to OpenClaw Gateway due to WebSocket origin check requirements (GitHub issues #9358, PR #10695). The security model requires proper Origin headers, but private IP connections trigger CVE-2026-25253 mitigations.

## Solution

ClawReach Bridge acts as a trusted proxy between ClawReach clients and OpenClaw Gateway:

- **Security**: Only accepts connections from Tailscale network (100.64.0.0/10)
- **Compatibility**: Injects proper Origin headers that Gateway requires
- **Zero Gateway changes**: Works with unmodified OpenClaw
- **Encrypted**: Tailscale provides end-to-end encryption
- **Web Admin UI**: Built-in browser dashboard for monitoring and control

## Quick Start

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/master/scripts/install.sh | bash

# 2. Setup (auto-detects Tailscale IP, writes config, starts service)
sudo clawreachbridge setup

# 3. Verify
curl http://127.0.0.1:8081/health

# 4. Open admin dashboard
open http://127.0.0.1:8081/ui/
```

The setup wizard will detect your Tailscale IP, prompt for Gateway URL and ports, write the config file, and optionally start the systemd service.

For manual installation, download the binary from [releases](https://github.com/cortexuvula/clawreachbridge/releases) and see `configs/config.example.yaml` for configuration reference.

## Architecture

```
ClawReach Client (mobile/web)
        ↓ (Tailscale, encrypted)
ClawReach Bridge (this project)
        ↓ (localhost, proper headers)
OpenClaw Gateway (unmodified)
```

## Web Admin UI

ClawReach Bridge ships with a built-in web dashboard — no extra install, no separate process. It starts automatically whenever the bridge is running.

**To use it:** open `http://127.0.0.1:8081/ui/` in any browser on the machine running the bridge.

The UI is embedded in the single binary via `go:embed` and served on the existing health listener. It provides five tabs:

### Dashboard

Real-time overview of bridge status: uptime, active/total connections, messages proxied, gateway reachability, memory usage, goroutine count, and build info. Auto-refreshes every 3 seconds.

### Connections

Per-IP connection breakdown sorted by count. Auto-refreshes every 5 seconds.

### Config

View and edit reloadable settings (log level, connection limits, rate limits, message size) directly from the browser. Changes are applied in-memory only and do not persist across restarts. Read-only settings (listen address, gateway URL, TLS) are displayed separately.

### Logs

Live log viewer with level filtering (debug/info/warn/error) and incremental auto-refresh. Displays up to 1000 recent log entries from a built-in ring buffer.

### Controls

- **Reload Config**: Reloads configuration from disk (equivalent to `kill -HUP`)
- **Restart Service**: Triggers a service restart via systemd (exits with code 1, systemd restarts)

### Security

The admin UI is served on the health listener (`127.0.0.1:8081`) which is localhost-only and not reachable from the network. Mutation endpoints (PUT, POST) require `Content-Type: application/json` to block browser form submissions. Auth token values are never exposed via the config API.

## API

The web UI is powered by a JSON API available at `/api/v1/` on the health listener:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/status` | Dashboard data (uptime, connections, memory, version) |
| GET | `/api/v1/connections` | Per-IP active connection breakdown |
| GET | `/api/v1/config` | Current config (reloadable + read-only, auth token masked) |
| PUT | `/api/v1/config` | Update reloadable config fields (in-memory only) |
| GET | `/api/v1/logs?limit=100&level=info&since=<RFC3339>` | Recent log entries from ring buffer |
| POST | `/api/v1/reload` | Reload config from disk |
| POST | `/api/v1/restart` | Restart service via systemd |

## Documentation

- [Implementation Plan](./IMPLEMENTATION_PLAN.md) - Comprehensive design document
- [Configuration Reference](./configs/config.example.yaml) - Example config with all options

## Requirements

- **Tailscale**: Must be installed and running
- **OpenClaw Gateway**: Must be running (typically on localhost:18800)
- **OS**: Linux (primary), macOS (supported)

## License

MIT (or Apache 2.0 - TBD)

## Contributing

See [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) § 16 for guidelines.

## Credits

Created by Andre Hugo ([@cortexuvula](https://github.com/cortexuvula)) to solve WebSocket origin issues for ClawReach mobile app.

Built with Claude Code Opus 4.6 from Fred's implementation plan.
