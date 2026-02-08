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

## Quick Start

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/master/scripts/install.sh | bash

# 2. Setup (auto-detects Tailscale IP, writes config, starts service)
sudo clawreachbridge setup

# 3. Verify
curl http://127.0.0.1:8081/health
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
