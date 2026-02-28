# AcaSkill VPN — Windows Daemon (Phase 2)

The Windows daemon is a Go background service that:
- Detects all active network interfaces (WiFi, Ethernet, USB modems, hotspots)
- Creates one WireGuard tunnel per selected interface
- Bonds them together with weighted traffic distribution
- Monitors link health every 500ms and auto-fails over dead links
- Exposes a local IPC socket on port 47821 for the GUI (Phase 3)

---

## Prerequisites

1. **Go 1.22+** — https://go.dev/dl/
2. **WireGuard for Windows** — https://www.wireguard.com/install/
3. A valid **AcaSkill VPN license key** from your server

---

## Build

Open PowerShell and run:

```powershell
cd client-windows
.\build.ps1
```

This produces:
- `build\acaskill-daemon.exe` — the background service
- `build\acaskill-cli.exe` — command line tool for testing

To build AND install as a Windows service (run as Administrator):

```powershell
.\build.ps1 -Install
```

---

## Configuration

On first run, a config file is created at:
```
C:\ProgramData\AcaSkillVPN\config.json
```

Edit it to add your license key and device name:

```json
{
  "apiBase": "https://api.acaskill.com",
  "vpnHost": "vpn.acaskill.com",
  "vpnPort": 51820,
  "licenseKey": "ACAS-XXXX-XXXX-XXXX-XXXX",
  "deviceId": "",
  "deviceName": "My PC",
  "heartbeatMs": 500,
  "failoverMs": 3000,
  "autoReconnect": true
}
```

**First time setup** — register your device with the server:

```powershell
acaskill-cli register "ACAS-XXXX-XXXX-XXXX-XXXX" "My PC"
```

---

## Running

**As a Windows Service** (recommended, auto-starts on boot):

```powershell
# Install (run as Administrator)
acaskill-daemon.exe install

# Start
Start-Service AcaSkillVPN

# Stop
Stop-Service AcaSkillVPN

# Uninstall
acaskill-daemon.exe uninstall
```

**Debug mode** (console, useful for development):

```powershell
acaskill-daemon.exe debug
```

---

## CLI Usage

```powershell
# Check connection status
acaskill-cli status

# List available network interfaces
acaskill-cli interfaces

# Connect a specific interface
acaskill-cli connect "Wi-Fi"
acaskill-cli connect "Ethernet"
acaskill-cli connect "Local Area Connection"

# Connect ALL available interfaces (maximum bonding)
acaskill-cli connect-all

# Disconnect everything
acaskill-cli disconnect-all
```

---

## Project Structure

```
client-windows/
├── cmd/
│   ├── daemon/
│   │   └── main.go          # Windows service entry point
│   └── cli/
│       └── main.go          # CLI tool for testing
├── internal/
│   ├── config/
│   │   └── config.go        # Config load/save
│   ├── interfaces/
│   │   └── detect.go        # Network interface detection
│   ├── wireguard/
│   │   └── manager.go       # WireGuard key gen + server provisioning
│   ├── bonding/
│   │   └── bonder.go        # Core bonding engine
│   └── ipc/
│       ├── server.go        # IPC server (daemon side)
│       └── client.go        # IPC client (GUI/CLI side)
├── build.ps1                # Build script
└── go.mod
```

---

## How Bonding Works

```
[MTN modem]  ──► WireGuard tunnel 1 (10.8.1.1) ──┐
[Glo modem]  ──► WireGuard tunnel 2 (10.8.1.2) ──┤──► Hetzner server ──► Internet
[WiFi]       ──► WireGuard tunnel 3 (10.8.1.3) ──┘
     ▲
     │
[Bonding daemon]
- Heartbeat every 500ms per tunnel
- Weighted round-robin based on latency
- Dead tunnel removed within 3 seconds
- New tunnel added automatically when interface appears
```

Traffic is distributed across all tunnels proportionally to their speed.
A 3 Mbps MTN + 2 Mbps Glo + 5 Mbps WiFi = ~10 Mbps combined.

---

## Logs

Daemon logs are written to:
```
C:\ProgramData\AcaSkillVPN\logs\daemon.log
```
