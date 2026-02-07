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
# Install (Linux/macOS)
curl -fsSL https://raw.githubusercontent.com/cortexuvula/clawreachbridge/master/scripts/install.sh | bash

# Or download binary from releases
wget https://github.com/cortexuvula/clawreachbridge/releases/latest/download/clawreachbridge-linux-amd64
chmod +x clawreachbridge-linux-amd64
sudo mv clawreachbridge-linux-amd64 /usr/local/bin/clawreachbridge

# Run interactive setup
clawreachbridge setup

# Start bridge
sudo systemctl start clawreachbridge

# Check status
curl http://127.0.0.1:8081/health
```

## Architecture

```
ClawReach Client (mobile/web)
        ↓ (Tailscale, encrypted)
ClawReach Bridge (this project)
        ↓ (localhost, proper headers)
OpenClaw Gateway (unmodified)
```

## Status

🚧 **Under Development** - See [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) for detailed design.

## Documentation

- [Implementation Plan](./IMPLEMENTATION_PLAN.md) - Comprehensive design document
- [Installation Guide](./docs/INSTALLATION.md) - Coming soon
- [Configuration Reference](./docs/CONFIGURATION.md) - Coming soon
- [Security Considerations](./docs/SECURITY.md) - Coming soon

## Requirements

- **Tailscale**: Must be installed and running
- **OpenClaw Gateway**: Must be running (typically on localhost:18800)
- **OS**: Linux (primary), macOS (supported), Windows (future)

## License

MIT (or Apache 2.0 - TBD)

## Contributing

See [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) § 16 for guidelines.

## Roadmap

- [ ] **v0.1.0**: Basic WebSocket proxy + Tailscale validation
- [ ] **v0.2.0**: Rate limiting, auth tokens, install script
- [ ] **v1.0.0**: Production-ready with full docs
- [ ] **v1.x**: Metrics, HA, Docker support

## Credits

Created by Andre Hugo ([@cortexuvula](https://github.com/cortexuvula)) to solve WebSocket origin issues for ClawReach mobile app.

Built with Claude Code Opus 4.6 from Fred's implementation plan.
