#!/bin/bash
# =============================================================================
# AcaSkill VPN - Hetzner Server Setup Script
# Run as root on a fresh Ubuntu 24.04 LTS VPS
# Usage: curl -sL https://api.acaskill.com/setup | bash
#   OR:  bash setup-server.sh
# =============================================================================

set -euo pipefail

# ── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'
BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
success() { echo -e "${GREEN}[OK]${RESET}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
error()   { echo -e "${RED}[ERROR]${RESET} $*"; exit 1; }
section() { echo -e "\n${BOLD}${CYAN}━━━ $* ━━━${RESET}"; }

# ── Validate root ─────────────────────────────────────────────────────────────
[[ $EUID -ne 0 ]] && error "This script must be run as root. Try: sudo bash setup-server.sh"

# ── Collect config ────────────────────────────────────────────────────────────
section "AcaSkill VPN Server Setup"
echo ""
echo "You will be asked a few questions. Press Enter to accept defaults."
echo ""

read -rp "  Domain for VPN server  [vpn.acaskill.com]: " VPN_DOMAIN
VPN_DOMAIN="${VPN_DOMAIN:-vpn.acaskill.com}"

read -rp "  Domain for API server  [api.acaskill.com]: " API_DOMAIN
API_DOMAIN="${API_DOMAIN:-api.acaskill.com}"

read -rp "  Admin email (for SSL)  : " ADMIN_EMAIL
[[ -z "$ADMIN_EMAIL" ]] && error "Admin email is required for SSL certificates."

read -rp "  WireGuard port         [51820]: " WG_PORT
WG_PORT="${WG_PORT:-51820}"

read -rp "  Aggregator port        [7878]:  " AGG_PORT
AGG_PORT="${AGG_PORT:-7878}"

# Generate secure secrets
DB_PASSWORD=$(openssl rand -hex 32)
API_SECRET=$(openssl rand -hex 32)
ADMIN_TOKEN=$(openssl rand -hex 24)

echo ""
info "Configuration:"
echo "  VPN Domain  : $VPN_DOMAIN"
echo "  API Domain  : $API_DOMAIN"
echo "  WG Port     : $WG_PORT"
echo "  Agg Port    : $AGG_PORT"
echo ""
read -rp "Proceed with installation? [y/N]: " CONFIRM
[[ "$CONFIRM" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 0; }

# ── Save config ───────────────────────────────────────────────────────────────
ENV_FILE="/opt/acaskill-vpn/.env"
mkdir -p /opt/acaskill-vpn

cat > "$ENV_FILE" <<EOF
VPN_DOMAIN=$VPN_DOMAIN
API_DOMAIN=$API_DOMAIN
ADMIN_EMAIL=$ADMIN_EMAIL
WG_PORT=$WG_PORT
AGG_PORT=$AGG_PORT
DB_PASSWORD=$DB_PASSWORD
API_SECRET=$API_SECRET
ADMIN_TOKEN=$ADMIN_TOKEN
EOF
chmod 600 "$ENV_FILE"
success "Config saved to $ENV_FILE"

# ── System updates ────────────────────────────────────────────────────────────
section "System Updates"
apt-get update -qq
apt-get upgrade -y -qq
apt-get install -y -qq \
    curl wget git ufw fail2ban \
    wireguard wireguard-tools \
    iptables-persistent net-tools \
    ca-certificates gnupg lsb-release
success "System packages installed"

# ── Docker ────────────────────────────────────────────────────────────────────
section "Installing Docker"
if ! command -v docker &>/dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
    success "Docker installed"
else
    success "Docker already installed"
fi

# ── Firewall ──────────────────────────────────────────────────────────────────
section "Configuring Firewall"
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh
ufw allow 80/tcp    comment 'HTTP (Let'\''s Encrypt)'
ufw allow 443/tcp   comment 'HTTPS'
ufw allow "$WG_PORT"/udp comment 'WireGuard'
ufw allow "$AGG_PORT"/tcp comment 'Aggregator'
ufw --force enable
success "Firewall configured"

# ── IP Forwarding ─────────────────────────────────────────────────────────────
section "Enabling IP Forwarding"
cat >> /etc/sysctl.conf <<'EOF'

# AcaSkill VPN - IP Forwarding
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
net.core.rmem_max=67108864
net.core.wmem_max=67108864
net.ipv4.tcp_rmem=4096 87380 67108864
net.ipv4.tcp_wmem=4096 65536 67108864
EOF
sysctl -p -q
success "IP forwarding enabled + buffers tuned"

# ── WireGuard Keys ────────────────────────────────────────────────────────────
section "Generating WireGuard Server Keys"
WG_DIR="/opt/acaskill-vpn/wireguard"
mkdir -p "$WG_DIR"
chmod 700 "$WG_DIR"

wg genkey | tee "$WG_DIR/server_private.key" | wg pubkey > "$WG_DIR/server_public.key"
chmod 600 "$WG_DIR/server_private.key"

WG_PRIVATE=$(cat "$WG_DIR/server_private.key")
WG_PUBLIC=$(cat "$WG_DIR/server_public.key")

# Append to env file
echo "WG_SERVER_PRIVATE_KEY=$WG_PRIVATE" >> "$ENV_FILE"
echo "WG_SERVER_PUBLIC_KEY=$WG_PUBLIC"   >> "$ENV_FILE"

success "WireGuard keys generated"
info "Server public key: $WG_PUBLIC"

# ── WireGuard Interface ───────────────────────────────────────────────────────
section "Creating WireGuard Interface"
SERVER_IFACE=$(ip route | grep default | awk '{print $5}' | head -1)
info "Detected main interface: $SERVER_IFACE"

cat > /etc/wireguard/wg0.conf <<EOF
[Interface]
Address    = 10.8.0.1/16
ListenPort = $WG_PORT
PrivateKey = $WG_PRIVATE

# NAT - forward client traffic to internet
PostUp   = iptables -t nat -A POSTROUTING -s 10.8.0.0/16 -o $SERVER_IFACE -j MASQUERADE
PostDown = iptables -t nat -D POSTROUTING -s 10.8.0.0/16 -o $SERVER_IFACE -j MASQUERADE

# Peers will be dynamically appended by the API
EOF
chmod 600 /etc/wireguard/wg0.conf

systemctl enable wg-quick@wg0
systemctl start  wg-quick@wg0
success "WireGuard interface wg0 started (10.8.0.1/16)"

# ── Copy app files ────────────────────────────────────────────────────────────
section "Deploying Application Stack"
cp -r "$(dirname "$0")/../server/"* /opt/acaskill-vpn/
cd /opt/acaskill-vpn

# ── Docker Compose Up ─────────────────────────────────────────────────────────
docker compose --env-file "$ENV_FILE" up -d --build
success "All services started"

# ── Fail2ban ──────────────────────────────────────────────────────────────────
section "Configuring Fail2ban"
cat > /etc/fail2ban/jail.local <<'EOF'
[DEFAULT]
bantime  = 3600
findtime = 600
maxretry = 5

[sshd]
enabled = true

[acaskill-api]
enabled  = true
port     = 443
filter   = acaskill-api
logpath  = /opt/acaskill-vpn/logs/api.log
maxretry = 10
EOF
systemctl enable --now fail2ban
success "Fail2ban configured"

# ── Print summary ─────────────────────────────────────────────────────────────
section "Setup Complete!"
echo ""
echo -e "  ${GREEN}${BOLD}✓ WireGuard VPN${RESET}      running on UDP :$WG_PORT"
echo -e "  ${GREEN}${BOLD}✓ Aggregator${RESET}         running on TCP :$AGG_PORT"
echo -e "  ${GREEN}${BOLD}✓ Licensing API${RESET}      https://$API_DOMAIN"
echo -e "  ${GREEN}${BOLD}✓ PostgreSQL${RESET}         running (internal)"
echo -e "  ${GREEN}${BOLD}✓ Caddy (HTTPS)${RESET}      https://$VPN_DOMAIN"
echo ""
echo -e "  ${YELLOW}${BOLD}⚠ DNS Records Needed (add in Porkbun):${RESET}"
echo -e "  Type: A  Name: vpn  Value: $(curl -s ifconfig.me)  TTL: 300"
echo -e "  Type: A  Name: api  Value: $(curl -s ifconfig.me)  TTL: 300"
echo ""
echo -e "  ${CYAN}Admin Token (save this!):${RESET}"
echo -e "  $ADMIN_TOKEN"
echo ""
echo -e "  ${CYAN}Full config saved at:${RESET} $ENV_FILE"
echo ""
success "AcaSkill VPN server is live. Add the DNS records above and you're done."
