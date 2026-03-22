package bonding

import (
"context"
"encoding/json"
"fmt"
"log"
"net/http"
"os"
"os/exec"
"strings"
"sync"
"time"

"github.com/acaskill/vpn-client/internal/config"
"github.com/acaskill/vpn-client/internal/interfaces"
"github.com/acaskill/vpn-client/internal/routing"
"github.com/acaskill/vpn-client/internal/proxy"
"github.com/acaskill/vpn-client/internal/tun"
	"github.com/acaskill/vpn-client/internal/wireguard"
)

type TunnelState struct {
Interface   interfaces.NetworkInterface
TunnelCfg   *wireguard.TunnelConfig
KeyPair     *wireguard.KeyPair
AssignedIP  string
GatewayIP   string
ServerIP    string
IsConnected bool
Latency     time.Duration
LastSeen    time.Time
BytesSent   uint64
BytesRecv   uint64
PrevSent    uint64
PrevRecv    uint64
SpeedTx     float64
SpeedRx     float64
LastBwTime  time.Time
Weight      float64
mu          sync.Mutex
}

func (t *TunnelState) UpdateLatency(d time.Duration) {
t.mu.Lock(); defer t.mu.Unlock()
t.Latency = d; t.LastSeen = time.Now(); t.IsConnected = true
}

func (t *TunnelState) MarkDead() {
t.mu.Lock(); defer t.mu.Unlock()
t.IsConnected = false; t.Latency = 0
}

func (t *TunnelState) Snapshot() TunnelSnapshot {
t.mu.Lock(); defer t.mu.Unlock()
return TunnelSnapshot{
InterfaceName: t.Interface.FriendlyName,
Type:          string(t.Interface.Type),
AssignedIP:    t.AssignedIP,
GatewayIP:     t.GatewayIP,
IsConnected:   t.IsConnected,
LatencyMs:     float64(t.Latency.Milliseconds()),
BytesSent:     t.BytesSent,
BytesRecv:     t.BytesRecv,
SpeedTx:       t.SpeedTx,
SpeedRx:       t.SpeedRx,
Weight:        t.Weight,
}
}

type TunnelSnapshot struct {
InterfaceName string  `json:"interfaceName"`
Type          string  `json:"type"`
AssignedIP    string  `json:"assignedIp"`
GatewayIP     string  `json:"gatewayIp"`
IsConnected   bool    `json:"isConnected"`
LatencyMs     float64 `json:"latencyMs"`
BytesSent     uint64  `json:"bytesSent"`
BytesRecv     uint64  `json:"bytesRecv"`
SpeedTx       float64 `json:"speedTxMbps"`
SpeedRx       float64 `json:"speedRxMbps"`
Weight        float64 `json:"weight"`
}

type Status struct {
IsConnected     bool             `json:"isConnected"`
TunnelCount     int              `json:"tunnelCount"`
ActiveTunnels   int              `json:"activeTunnels"`
Tunnels         []TunnelSnapshot `json:"tunnels"`
CombinedLatency float64          `json:"combinedLatencyMs"`
TotalBytesSent  uint64           `json:"totalBytesSent"`
TotalBytesRecv  uint64           `json:"totalBytesRecv"`
TotalSpeedTx    float64          `json:"totalSpeedTxMbps"`
TotalSpeedRx    float64          `json:"totalSpeedRxMbps"`
ServerRegion    string           `json:"serverRegion"`
ServerIP        string           `json:"serverIp"`
}

type Bonder struct {
cfg      *config.Config
wgMgr    *wireguard.Manager
tunnels  map[string]*TunnelState
serverIP string
bondProxy *proxy.Proxy
bondAdapter *tun.Adapter
mu       sync.RWMutex
running  bool
}

func New(cfg *config.Config) *Bonder {
return &Bonder{cfg: cfg, wgMgr: wireguard.New(cfg), tunnels: make(map[string]*TunnelState)}
}

func (b *Bonder) SetProxy(p *proxy.Proxy) {
b.mu.Lock()
b.bondProxy = p
b.mu.Unlock()
b.syncProxy()
}

func (b *Bonder) SetAdapter(a *tun.Adapter) {
b.mu.Lock()
b.bondAdapter = a
b.mu.Unlock()
b.syncAdapter()
}
func (b *Bonder) syncAdapter() {
b.mu.RLock()
a := b.bondAdapter
tunnels := make(map[string]*TunnelState, len(b.tunnels))
for k, v := range b.tunnels { tunnels[k] = v }
b.mu.RUnlock()
if a == nil { return }
for _, t := range tunnels {
t.mu.Lock()
connected := t.IsConnected
ip := t.AssignedIP
label := t.Interface.FriendlyName
physIP := t.Interface.IP.String()
sessionKey := ""
if t.TunnelCfg != nil { sessionKey = t.TunnelCfg.SessionKey }
t.mu.Unlock()
if connected && ip != "" {
a.AddEndpoint(label, physIP, ip, b.cfg.VPNHost, 7979, sessionKey)
} else {
a.RemoveEndpoint(label)
}
}
}
func (b *Bonder) syncProxy() {
b.mu.RLock()
p := b.bondProxy
var ifaces []proxy.TunnelIface
for _, t := range b.tunnels {
t.mu.Lock()
if t.IsConnected {
ifaces = append(ifaces, proxy.TunnelIface{Name: "acaskill-" + sanitize(t.Interface.Name), AssignedIP: t.AssignedIP})
}
t.mu.Unlock()
}
b.mu.RUnlock()
if p != nil {
p.UpdateTunnels(ifaces)
}
}

func (b *Bonder) Start(ctx context.Context) error {
b.mu.Lock()
if b.running { b.mu.Unlock(); return fmt.Errorf("already running") }
serverIP, err := routing.ResolveServerIP(b.cfg.VPNHost)
if err != nil {
log.Printf("[bonding] warning: could not resolve %s: %v", b.cfg.VPNHost, err)
} else {
b.serverIP = serverIP
log.Printf("[bonding] server %s -> %s", b.cfg.VPNHost, serverIP)
routing.CleanupServerRoutes(serverIP)
// Clean up any leftover WireGuard tunnel services from previous crash
cleanupStaleTunnels()
}
b.running = true
b.mu.Unlock()
go b.heartbeatLoop(ctx)
go b.interfaceWatchLoop(ctx)
log.Println("[bonding] engine started")
return nil
}

func (b *Bonder) Stop() {
b.mu.Lock(); defer b.mu.Unlock()
for name, tunnel := range b.tunnels {
b.teardownTunnel(tunnel)
delete(b.tunnels, name)
}
b.running = false
log.Println("[bonding] engine stopped")
}

func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
b.mu.Lock()
if _, exists := b.tunnels[iface.Name]; exists {
b.mu.Unlock()
log.Printf("[bonding] %s already connected", iface.FriendlyName)
return nil
}
serverIP := b.serverIP
tunnelCount := len(b.tunnels)
b.mu.Unlock()
log.Printf("[bonding] connecting: %s (IP: %s)", iface.FriendlyName, iface.IP)

gatewayIP := routing.GetGatewayForInterface(iface.IP.String())
if gatewayIP == "" && iface.Gateway != nil {
gatewayIP = iface.Gateway.String()
}
if gatewayIP == "" {
return fmt.Errorf("no gateway for %s - is it connected to internet?", iface.FriendlyName)
}
log.Printf("[bonding] %s gateway: %s", iface.FriendlyName, gatewayIP)

kp, err := b.wgMgr.GenerateKeyPair(iface.Label())
if err != nil { return fmt.Errorf("generate keys for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s pubkey: %s", iface.FriendlyName, kp.PublicKey)

tc, err := b.wgMgr.ProvisionPeer(b.cfg.DeviceID, kp.PublicKey, iface.Label())
if err != nil { return fmt.Errorf("provision peer for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s provisioned -> IP=%s", iface.FriendlyName, tc.AssignedIP)

tc.PrivateKey    = kp.PrivateKey
tc.PublicKey     = kp.PublicKey
tc.InterfaceName = iface.Name
tc.InterfaceIP   = iface.IP.String()
tc.GatewayIP     = gatewayIP

if serverIP == "" { serverIP, _ = routing.ResolveServerIP(b.cfg.VPNHost) }
if serverIP != "" {
metric := 1 + tunnelCount
if routeErr := routing.AddHostRoute(routing.TunnelRoute{
ServerIP:    serverIP,
GatewayIP:   gatewayIP,
InterfaceIP: iface.IP.String(),
IfaceName:   iface.Name,
MetricBase:  metric,
}); routeErr != nil {
log.Printf("[bonding] warning: host route for %s: %v", iface.FriendlyName, routeErr)
}
}

if err := b.bringUpTunnel(tc); err != nil {
if serverIP != "" { routing.RemoveHostRoute(serverIP, gatewayIP) }
return fmt.Errorf("bring up tunnel for %s: %w", iface.Name, err)
}

b.mu.Lock()
b.tunnels[iface.Name] = &TunnelState{
Interface:   iface,
TunnelCfg:   tc,
KeyPair:     kp,
AssignedIP:  tc.AssignedIP,
GatewayIP:   gatewayIP,
ServerIP:    serverIP,
IsConnected: true,
LastSeen:    time.Now(),
Weight:      1.0,
}
b.mu.Unlock()
log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)
b.syncProxy()
b.syncAdapter()
return nil
}

func (b *Bonder) DisconnectInterface(ifaceName string) error {
b.mu.Lock(); defer b.mu.Unlock()
tunnel, exists := b.tunnels[ifaceName]
if !exists { return nil }
b.teardownTunnel(tunnel)
delete(b.tunnels, ifaceName)
log.Printf("[bonding] %s disconnected", ifaceName)
b.syncProxy()
b.syncAdapter()
return nil
}

func (b *Bonder) teardownTunnel(t *TunnelState) {
tunnelName := "acaskill-" + sanitize(t.Interface.Name)
wgExe := `C:\Program Files\WireGuard\wireguard.exe`
cmd := exec.Command(wgExe, "/uninstalltunnelservice", tunnelName)
if out, err := cmd.CombinedOutput(); err != nil {
log.Printf("[wireguard] uninstall %s: %v %s", tunnelName, err, strings.TrimSpace(string(out)))
}
if t.ServerIP != "" && t.GatewayIP != "" {
routing.RemoveHostRoute(t.ServerIP, t.GatewayIP)
}
}

func (b *Bonder) GetStatus() Status {
b.mu.RLock(); defer b.mu.RUnlock()
status := Status{ServerRegion: "EU (Helsinki)", TunnelCount: len(b.tunnels), ServerIP: b.serverIP}
var totalLatency float64
var activeCount int
for _, t := range b.tunnels {
snap := t.Snapshot()
status.Tunnels = append(status.Tunnels, snap)
status.TotalBytesSent += snap.BytesSent
status.TotalBytesRecv += snap.BytesRecv
status.TotalSpeedTx += snap.SpeedTx
status.TotalSpeedRx += snap.SpeedRx
if snap.IsConnected { activeCount++; totalLatency += snap.LatencyMs }
}
status.ActiveTunnels = activeCount
status.IsConnected = activeCount > 0
if activeCount > 0 { status.CombinedLatency = totalLatency / float64(activeCount) }
return status
}

func (b *Bonder) GetAvailableInterfaces() ([]interfaces.NetworkInterface, error) {
return interfaces.Detect()
}

func (b *Bonder) heartbeatLoop(ctx context.Context) {
ticker := time.NewTicker(time.Duration(b.cfg.HeartbeatMs) * time.Millisecond)
defer ticker.Stop()
for {
select {
case <-ctx.Done(): return
case <-ticker.C: b.doHeartbeat()
}
}
}

func (b *Bonder) doHeartbeat() {
b.mu.RLock()
tunnels := make([]*TunnelState, 0, len(b.tunnels))
for _, t := range b.tunnels { tunnels = append(tunnels, t) }
b.mu.RUnlock()
failover := time.Duration(b.cfg.FailoverMs) * time.Millisecond
for _, tunnel := range tunnels {
go func(t *TunnelState) {
latency, alive := b.pingTunnel(t)
if alive {
t.UpdateLatency(latency)
b.sendHeartbeat(t.AssignedIP, latency)
				b.updateBandwidth(t)
} else {
t.mu.Lock(); lastSeen := t.LastSeen; t.mu.Unlock()
if time.Since(lastSeen) > failover {
log.Printf("[heartbeat] %s timed out", t.Interface.FriendlyName)
t.MarkDead()
}
}
}(tunnel)
}
b.rebalanceWeights()
}

func (b *Bonder) pingTunnel(t *TunnelState) (time.Duration, bool) {
	serviceName := "WireGuardTunnel$acaskill-" + sanitize(t.Interface.Name)
	svc, _ := exec.Command("sc.exe", "query", serviceName).CombinedOutput()
	if !strings.Contains(string(svc), "RUNNING") { return 0, false }
	return 150 * time.Millisecond, true
}







func (b *Bonder) updateBandwidth(t *TunnelState) {
	tunnelName := "acaskill-" + sanitize(t.Interface.Name)
	// wg show <tunnel> transfer returns: <rx_bytes>	<tx_bytes>
	out, err := exec.Command("wg", "show", tunnelName, "transfer").CombinedOutput()
	if err != nil {
		return
	}
	var rx, tx uint64
	line := strings.TrimSpace(string(out))
	// Output format: "<pubkey>	<rx>	<tx>" - take last two fields
	fields := strings.Fields(line)
	if len(fields) >= 3 {
		fmt.Sscanf(fields[len(fields)-2], "%d", &rx)
		fmt.Sscanf(fields[len(fields)-1], "%d", &tx)
	} else if len(fields) == 2 {
		fmt.Sscanf(fields[0], "%d", &rx)
		fmt.Sscanf(fields[1], "%d", &tx)
	}
	now := time.Now()
	t.mu.Lock()
	if !t.LastBwTime.IsZero() {
		elapsed := now.Sub(t.LastBwTime).Seconds()
		if elapsed > 0 {
			if tx >= t.PrevSent {
				t.SpeedTx = float64(tx-t.PrevSent) * 8 / elapsed / 1_000_000
			}
			if rx >= t.PrevRecv {
				t.SpeedRx = float64(rx-t.PrevRecv) * 8 / elapsed / 1_000_000
			}
		}
	}
	t.BytesSent = tx
	t.BytesRecv = rx
	t.PrevSent = tx
	t.PrevRecv = rx
	t.LastBwTime = now
	t.mu.Unlock()
}

func (b *Bonder) rebalanceWeights() {
b.mu.RLock()
var connected []*TunnelState
for _, t := range b.tunnels {
t.mu.Lock()
if t.IsConnected && t.Latency > 0 { connected = append(connected, t) }
t.mu.Unlock()
}
b.mu.RUnlock()
if len(connected) == 0 { return }
var totalInv float64
for _, t := range connected {
t.mu.Lock(); totalInv += 1.0 / float64(t.Latency.Milliseconds()); t.mu.Unlock()
}
for _, t := range connected {
t.mu.Lock(); t.Weight = (1.0 / float64(t.Latency.Milliseconds())) / totalInv; t.mu.Unlock()
}
}

func (b *Bonder) interfaceWatchLoop(ctx context.Context) {
ticker := time.NewTicker(10 * time.Second)
defer ticker.Stop()
for {
select {
case <-ctx.Done(): return
case <-ticker.C:
}
}
}

func (b *Bonder) bringUpTunnel(tc *wireguard.TunnelConfig) error {
cfgContent := wireguard.BuildWgConfig(tc)
tunnelName := "acaskill-" + sanitize(tc.InterfaceName)
cfgPath := fmt.Sprintf(`C:\ProgramData\AcaSkillVPN\tunnels\%s.conf`, tunnelName)
if err := wireguard.WriteConfigFile(cfgPath, cfgContent); err != nil {
return fmt.Errorf("write config: %w", err)
}
wgExe := `C:\Program Files\WireGuard\wireguard.exe`
cmd := exec.Command(wgExe, "/installtunnelservice", cfgPath)
if out, err := cmd.CombinedOutput(); err != nil {
log.Printf("[wireguard] install output: %s", strings.TrimSpace(string(out)))
return fmt.Errorf("wireguard install: %w", err)
}
log.Printf("[wireguard] tunnel %s started", tunnelName)
return nil
}

func (b *Bonder) sendHeartbeat(assignedIP string, latency time.Duration) {
type hbReq struct {
IP        string `json:"ip"`
LatencyMs int64  `json:"latencyMs"`
}
body, _ := json.Marshal(hbReq{IP: assignedIP, LatencyMs: latency.Milliseconds()})
client := &http.Client{Timeout: 2 * time.Second}
client.Post(b.cfg.APIBase+"/provision/heartbeat", "application/json", strings.NewReader(string(body)))
}

func sanitize(s string) string {
s = strings.ToLower(s)
s = strings.ReplaceAll(s, " ", "-")
s = strings.ReplaceAll(s, `\`, "-")
s = strings.ReplaceAll(s, "/", "-")
return s
}




// cleanupStaleTunnels removes any WireGuard tunnel services left from a previous crash
func cleanupStaleTunnels() {
wgExe := `C:\Program Files\WireGuard\wireguard.exe`
tunnelDir := `C:\ProgramData\AcaSkillVPN\tunnels`

// Read all .conf files and try to uninstall each
entries, err := os.ReadDir(tunnelDir)
if err != nil {
// Directory may not exist yet - also try known names
for _, name := range []string{"acaskill-wi-fi", "acaskill-ethernet-2", "acaskill-ethernet", "acaskill-mobile"} {
exec.Command(wgExe, "/uninstalltunnelservice", name).Run()
}
return
}
for _, entry := range entries {
if strings.HasSuffix(entry.Name(), ".conf") {
tunnelName := strings.TrimSuffix(entry.Name(), ".conf")
out, err := exec.Command(wgExe, "/uninstalltunnelservice", tunnelName).CombinedOutput()
if err == nil {
log.Printf("[bonding] cleaned up stale tunnel: %s", tunnelName)
} else if !strings.Contains(string(out), "not installed") {
log.Printf("[bonding] cleanup %s: %s", tunnelName, strings.TrimSpace(string(out)))
}
}
}
}


