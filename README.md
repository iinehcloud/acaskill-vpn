# AcaSkill VPN — Channel Bonding Solution

Bond multiple Nigerian mobile networks (MTN, Glo, Airtel) + WiFi + Ethernet
into a single fast, reliable connection via a EU server.

## Project Structure

```
acaskill-vpn/
├── server/          # Phase 1 - Hetzner server stack (THIS PHASE)
├── client-windows/  # Phase 2+3 - Windows GUI app (coming next)
├── scripts/         # Deployment scripts
└── docs/            # Documentation
```

## Build Phases

| Phase | Component | Status |
|-------|-----------|--------|
| 1 | Server (WireGuard + API + Aggregator + DB) | ✅ Complete |
| 2 | Windows bonding daemon (Go service) | 🔜 Next |
| 3 | Windows GUI (Tauri + React) | 🔜 Planned |
| 4 | Licensing system integration | 🔜 Planned |
| 5 | Installer + auto-updater | 🔜 Planned |

## Quick Start

See [docs/PHASE1-SERVER.md](docs/PHASE1-SERVER.md) for full server deployment guide.
