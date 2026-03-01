package bonding

import (
"context"
"encoding/json"
"fmt"
"log"
"net/http"
"os/exec"
"strings"
"sync"
"time"

"github.com/acaskill/vpn-client/internal/config"
"github.com/acaskill/vpn-client/internal/interfaces"
"github.com/acaskill/vpn-client/internal/wireguard"
)

type TunnelState struct {
Interface   interfaces.NetworkInterface
TunnelCfg   *wireguard.TunnelConfig
KeyPair     *wireguard.KeyPair
AssignedIP  string
IsConnected bool
Latency     time.Duration
LastSeen    time.Time
BytesSent   uint64
BytesRecv   uint64
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
IsConnected:   t.IsConnected,
LatencyMs:     float64(t.Latency.Milliseconds()),
BytesSent:     t.BytesSent,
BytesRecv:     t.BytesRecv,
Weight:        t.Weight,
}
}

type TunnelSnapshot struct {
InterfaceName string  `json:"interfaceName"`
Type          string  `json:"type"`
AssignedIP    string  `json:"assignedIp"`
IsConnected   bool    `json:"isConnected"`
LatencyMs     float64 `json:"latencyMs"`
BytesSent     uint64  `json:"bytesSent"`
BytesRecv     uint64  `json:"bytesRecv"`
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
ServerRegion    string           `json:"serverRegion"`
}

type Bonder struct {
cfg     *config.Config
wgMgr   *wireguard.Manager
tunnels map[string]*TunnelState
mu      sync.RWMutex
running bool
}

func New(cfg *config.Config) *Bonder {
return &Bonder{cfg: cfg, wgMgr: wireguard.New(cfg), tunnels: make(map[string]*TunnelState)}
}

func (b *Bonder) Start(ctx context.Context) error {
b.mu.Lock()
if b.running { b.mu.Unlock(); return fmt.Errorf("already running") }
b.running = true
b.mu.Unlock()
go b.heartbeatLoop(ctx)
log.Println("[bonding] engine started")
return nil
}

func (b *Bonder) Stop() {
b.mu.Lock(); defer b.mu.Unlock()
for name, tunnel := range b.tunnels {
b.bringDownTunnel(tunnel)
delete(b.tunnels, name)
}
b.running = false
log.Println("[bonding] engine stopped")
}

func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
b.mu.Lock(); defer b.mu.Unlock()
if _, exists := b.tunnels[iface.Name]; exists { return nil }

log.Printf("[bonding] connecting interface: %s", iface.FriendlyName)
log.Printf("[bonding] licenseKey=%q deviceID=%q", b.cfg.LicenseKey, b.cfg.DeviceID)

kp, err := b.wgMgr.GenerateKeyPair(iface.Label())
if err != nil {
log.Printf("[bonding] ERROR generating keys for %s: %v", iface.Name, err)
return fmt.Errorf("generate keys: %w", err)
}
log.Printf("[bonding] generated pubkey for %s: %s", iface.FriendlyName, kp.PublicKey)

tc, err := b.wgMgr.ProvisionPeer(b.cfg.DeviceID, kp.PublicKey, iface.Label())
if err != nil {
log.Printf("[bonding] ERROR provisioning peer for %s: %v", iface.Name, err)
return fmt.Errorf("provision peer: %w", err)
}
log.Printf("[bonding] provisioned %s -> assignedIP=%s", iface.FriendlyName, tc.AssignedIP)

tc.PrivateKey    = kp.PrivateKey
tc.PublicKey     = kp.PublicKey
tc.InterfaceName = iface.Name
tc.InterfaceIP   = iface.IP.String()

if err := b.bringUpTunnel(tc); err != nil {
log.Printf("[bonding] ERROR bringing up tunnel for %s: %v", iface.Name, err)
return fmt.Errorf("bring up tunnel: %w", err)
}

b.tunnels[iface.Name] = &TunnelState{
Interface:   iface,
TunnelCfg:   tc,
KeyPair:     kp,
AssignedIP:  tc.AssignedIP,
IsConnected: true,
LastSeen:    time.Now(),
Weight:      1.0,
}
log.Printf("[bonding] interface %s connected (IP: %s)", iface.FriendlyName, tc.AssignedIP)
return nil
}

func (b *Bonder) DisconnectInterface(ifaceName string) error {
b.mu.Lock(); defer b.mu.Unlock()
tunnel, exists := b.tunnels[ifaceName]
if !exists { return nil }
b.bringDownTunnel(tunnel)
delete(b.tunnels, ifaceName)
log.Printf("[bonding] interface %s disconnected", ifaceName)
return nil
}

func (b *Bonder) GetStatus() Status {
b.mu.RLock(); defer b.mu.RUnlock()
status := Status{ServerRegion: "EU (Helsinki)", TunnelCount: len(b.tunnels)}
var totalLatency float64
var activeCount int
for _, t := range b.tunnels {
snap := t.Snapshot()
status.Tunnels = append(status.Tunnels, snap)
status.TotalBytesSent += snap.BytesSent
status.TotalBytesRecv += snap.BytesRecv
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
} else {
t.mu.Lock(); lastSeen := t.LastSeen; t.mu.Unlock()
if time.Since(lastSeen) > failover { t.MarkDead() }
}
}(tunnel)
}
b.rebalanceWeights()
}

func (b *Bonder) pingTunnel(t *TunnelState) (time.Duration, bool) {
start := time.Now()
client := &http.Client{Timeout: 2 * time.Second}
resp, err := client.Get(b.cfg.APIBase + "/health")
if err != nil { return 0, false }
resp.Body.Close()
return time.Since(start), resp.StatusCode == 200
}

func (b *Bonder) rebalanceWeights() {
b.mu.RLock()
var tunnels []*TunnelState
for _, t := range b.tunnels {
t.mu.Lock()
if t.IsConnected && t.Latency > 0 { tunnels = append(tunnels, t) }
t.mu.Unlock()
}
b.mu.RUnlock()
if len(tunnels) == 0 { return }
var total float64
for _, t := range tunnels { t.mu.Lock(); total += 1.0 / float64(t.Latency.Milliseconds()); t.mu.Unlock() }
for _, t := range tunnels { t.mu.Lock(); t.Weight = (1.0 / float64(t.Latency.Milliseconds())) / total; t.mu.Unlock() }
}

func (b *Bonder) bringUpTunnel(tc *wireguard.TunnelConfig) error {
cfgContent := wireguard.BuildWgConfig(tc)
tunnelName := "acaskill-" + sanitize(tc.InterfaceName)
cfgPath := fmt.Sprintf(`C:\ProgramData\AcaSkillVPN\tunnels\%s.conf`, tunnelName)

exec.Command("cmd", "/C", `mkdir "C:\ProgramData\AcaSkillVPN\tunnels" 2>nul`).Run()

if err := wireguard.WriteConfigFile(cfgPath, cfgContent); err != nil {
return fmt.Errorf("write tunnel config: %w", err)
}

wgExe := `C:\Program Files\WireGuard\wireguard.exe`
cmd := exec.Command(wgExe, "/installtunnelservice", cfgPath)
if out, err := cmd.CombinedOutput(); err != nil {
log.Printf("[wireguard] install output: %s", out)
return fmt.Errorf("wireguard install: %w", err)
}
log.Printf("[wireguard] tunnel %s started", tunnelName)
return nil
}

func (b *Bonder) bringDownTunnel(t *TunnelState) {
tunnelName := "acaskill-" + sanitize(t.Interface.Name)
cmd := exec.Command(`C:\Program Files\WireGuard\wireguard.exe`, "/uninstalltunnelservice", tunnelName)
if out, err := cmd.CombinedOutput(); err != nil {
log.Printf("[wireguard] uninstall %s: %v\n%s", tunnelName, err, out)
}
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
s = strings.ReplaceAll(s, "\\", "-")
s = strings.ReplaceAll(s, "/", "-")
return s
}





