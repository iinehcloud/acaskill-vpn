# AcaSkill VPN — Server Deployment Guide (Phase 1)

## Overview

This is the server-side stack for AcaSkill VPN. It runs on a single Hetzner VPS
and provides:

- **WireGuard VPN** — encrypted tunnels from Windows clients
- **Bonding Aggregator** — combines multiple client tunnels into one logical connection
- **Licensing API** — validates license keys and provisions WireGuard peers
- **PostgreSQL** — stores licenses, devices, and peer assignments
- **Caddy** — automatic HTTPS with Let's Encrypt for acaskill.com subdomains

---

## Prerequisites

### 1. Hetzner VPS
- **Server type**: CX31 (4 vCPU, 8GB RAM, 20TB bandwidth) — ~€10/month
- **OS**: Ubuntu 24.04 LTS
- **Location**: Falkenstein or Helsinki (EU, low latency)

**Create your server:**
1. Go to https://console.hetzner.cloud
2. Create account → New Project → Add Server
3. Select: Ubuntu 24.04, CX31, Falkenstein (FSN1)
4. Add your SSH public key
5. Create server — note the IP address

### 2. DNS Records (Porkbun)
Add these in your Porkbun dashboard (DNS → acaskill.com):

| Type | Name | Value           | TTL |
|------|------|-----------------|-----|
| A    | vpn  | YOUR_SERVER_IP  | 300 |
| A    | api  | YOUR_SERVER_IP  | 300 |

Wait 1-5 minutes for DNS to propagate before running the setup script.

---

## Deployment

### Step 1 — Clone the repo onto the server
```bash
ssh root@YOUR_SERVER_IP
git clone https://github.com/acaskill/vpn-server.git /opt/acaskill-vpn-src
cd /opt/acaskill-vpn-src
```

### Step 2 — Run the setup script
```bash
chmod +x scripts/setup-server.sh
bash scripts/setup-server.sh
```

The script will ask for:
- Your domain names (defaults: vpn.acaskill.com, api.acaskill.com)
- Your admin email (for SSL certificate)
- WireGuard port (default: 51820)
- Aggregator port (default: 7878)

It then automatically:
- Updates the system
- Installs Docker + WireGuard
- Configures the firewall
- Generates WireGuard server keys
- Starts all Docker services
- Issues SSL certificates via Let's Encrypt

### Step 3 — Save your admin token
At the end of the script, you'll see your **Admin Token**. Save this securely —
you'll need it to create license keys from the admin API.

---

## Verifying the Installation

```bash
# Check all containers are running
docker compose ps

# Check WireGuard is up
wg show

# Test the API
curl https://api.acaskill.com/health

# Check logs
docker compose logs api
docker compose logs aggregator
```

---

## Managing Licenses

### Create a new license key
```bash
curl -X POST https://api.acaskill.com/admin/license \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "standard",
    "maxDevices": 3,
    "notes": "Customer: John Doe"
  }'
```

Response:
```json
{
  "ok": true,
  "license": {
    "id": "...",
    "key": "ACAS-XXXX-XXXX-XXXX-XXXX",
    "plan": "standard",
    "max_devices": 3
  }
}
```

### List all licenses
```bash
curl https://api.acaskill.com/admin/licenses \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

### View server stats
```bash
curl https://api.acaskill.com/admin/stats \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

### Deactivate a license
```bash
curl -X PATCH https://api.acaskill.com/admin/license/LICENSE_ID \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"isActive": false}'
```

---

## Architecture

```
Internet
    │
    ▼
[Caddy :443]  ← HTTPS, auto SSL, rate limiting
    │
    ├── api.acaskill.com → [Node.js API :3000]
    │                           │
    │                      [PostgreSQL]
    │
[WireGuard :51820/udp]  ← client tunnels
    │
    ▼
[Go Aggregator :7878]  ← bonds multiple tunnels per device
    │
    ▼
  NAT → Internet
```

---

## File Structure

```
server/
├── docker-compose.yml      # All services
├── caddy/
│   └── Caddyfile           # HTTPS config
├── postgres/
│   └── init.sql            # Database schema
├── api/
│   ├── Dockerfile
│   ├── package.json
│   └── src/
│       ├── index.js        # Fastify app entry
│       ├── db.js           # DB connection
│       └── routes/
│           ├── health.js
│           ├── license.js  # License validate/register
│           ├── provision.js # WireGuard peer management
│           └── admin.js    # Admin CRUD
└── aggregator/
    ├── Dockerfile
    ├── go.mod
    └── main.go             # Bonding aggregator
```

---

## Maintenance

### Update all services
```bash
cd /opt/acaskill-vpn
git pull
docker compose up -d --build
```

### Backup database
```bash
docker exec acaskill_db pg_dump -U acaskill acaskill_vpn > backup-$(date +%Y%m%d).sql
```

### View live logs
```bash
docker compose logs -f api
docker compose logs -f aggregator
```

### Restart a service
```bash
docker compose restart api
```

---

## Scaling (Future)

When you need more capacity:
1. **Upgrade Hetzner server**: CX31 → CX41 (8 vCPU, 16GB) — no reinstall needed
2. **Second EU node**: Deploy identical stack in Amsterdam (NBG1) for redundancy
3. **Load balancer**: Add Hetzner Load Balancer (€6/mo) in front of both nodes

The architecture is designed to scale horizontally — each server is stateless
(DB is the single source of truth) so adding nodes is straightforward.

---

## Security Notes

- WireGuard uses ChaCha20-Poly1305 encryption — military grade
- Client private keys are generated on the client device, never sent to server
- Admin API requires bearer token authentication
- Caddy automatically renews SSL certificates
- Fail2ban blocks repeated failed attempts
- No traffic logs stored (privacy-first design)
- PostgreSQL not exposed to the internet (Docker internal network only)
