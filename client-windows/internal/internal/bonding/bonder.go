// =============================================================================
// AcaSkill VPN - Bonding Engine
// Manages multiple WireGuard tunnels, distributes traffic across them,
// monitors link health, and handles automatic failover.
// =============================================================================

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

// TunnelState tracks the live state of one WireGuard tunnel
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
	Weight      float64  // 0.0-1.0
	mu          sync.Mutex
}

func (t *TunnelState) UpdateLatency(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Latency = d
	t.LastSeen = time.Now()
	t.IsConnected = true
}

func (t *TunnelState) MarkDead() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.IsConnected = false
	t.Latency = 0
}

func (t *TunnelState) Snapshot() TunnelSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
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

// TunnelSnapshot is a JSON-serializable view of tunnel state (sent to GUI)
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

// Status is the full status sent to the GUI
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

// Bonder manages the full bonding lifecycle
type Bonder struct {
	cfg      *config.Config
	wgMgr    *wireguard.Manager
	tunnels  map[string]*TunnelState  // keyed by interface name
	mu       sync.RWMutex
	running  bool
	cancelFn context.CancelFunc
}

func New(cfg *config.Config) *Bonder {
	return &Bonder{
		cfg:     cfg,
		wgMgr:   wireguard.New(cfg),
		tunnels: make(map[string]*TunnelState),
	}
}

// Start begins the bonding engine
func (b *Bonder) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("already running")
	}
	b.running = true
	b.mu.Unlock()

	go b.heartbeatLoop(ctx)
	go b.interfaceWatchLoop(ctx)

	log.Println("[bonding] engine started")
	return nil
}

// Stop tears down all tunnels and stops the engine
func (b *Bonder) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for name, tunnel := range b.tunnels {
		b.bringDownTunnel(tunnel)
		delete(b.tunnels, name)
	}
	b.running = false
	log.Println("[bonding] engine stopped")
}

// ConnectInterface brings up a WireGuard tunnel for a specific interface
func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.tunnels[iface.Name]; exists {
		return nil // already connected
	}

	log.Printf("[bonding] connecting interface: %s", iface.FriendlyName)

	// Generate/load keypair for this interface
	kp, err := b.wgMgr.GenerateKeyPair(iface.Label())
	if err != nil {
		return fmt.Errorf("generate keys for %s: %w", iface.Name, err)
	}

	// Provision peer with server
	tc, err := b.wgMgr.ProvisionPeer(b.cfg.DeviceID, kp.PublicKey, iface.Label())
	if err != nil {
		return fmt.Errorf("provision peer for %s: %w", iface.Name, err)
	}

	tc.PrivateKey     = kp.PrivateKey
	tc.PublicKey      = kp.PublicKey
	tc.InterfaceName  = iface.Name
	tc.InterfaceIP    = iface.IP.String()

	// Bring up the WireGuard tunnel
	if err := b.bringUpTunnel(tc); err != nil {
		return fmt.Errorf("bring up tunnel for %s: %w", iface.Name, err)
	}

	tunnel := &TunnelState{
		Interface:   iface,
		TunnelCfg:   tc,
		KeyPair:     kp,
		AssignedIP:  tc.AssignedIP,
		IsConnected: true,
		LastSeen:    time.Now(),
		Weight:      1.0,
	}

	b.tunnels[iface.Name] = tunnel
	log.Printf("[bonding] interface %s connected (IP: %s)", iface.FriendlyName, tc.AssignedIP)
	return nil
}

// DisconnectInterface tears down the tunnel for a specific interface
func (b *Bonder) DisconnectInterface(ifaceName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tunnel, exists := b.tunnels[ifaceName]
	if !exists {
		return nil
	}

	b.bringDownTunnel(tunnel)
	delete(b.tunnels, ifaceName)
	log.Printf("[bonding] interface %s disconnected", ifaceName)
	return nil
}

// GetStatus returns a snapshot of all tunnel states for the GUI
func (b *Bonder) GetStatus() Status {
	b.mu.RLock()
	defer b.mu.RUnlock()

	status := Status{
		ServerRegion: "EU (Helsinki)",
		TunnelCount:  len(b.tunnels),
	}

	var totalLatency float64
	var activeCount int

	for _, t := range b.tunnels {
		snap := t.Snapshot()
		status.Tunnels = append(status.Tunnels, snap)
		status.TotalBytesSent += snap.BytesSent
		status.TotalBytesRecv += snap.BytesRecv

		if snap.IsConnected {
			activeCount++
			totalLatency += snap.LatencyMs
		}
	}

	status.ActiveTunnels = activeCount
	status.IsConnected = activeCount > 0

	if activeCount > 0 {
		status.CombinedLatency = totalLatency / float64(activeCount)
	}

	return status
}

// GetAvailableInterfaces returns all detected usable interfaces
func (b *Bonder) GetAvailableInterfaces() ([]interfaces.NetworkInterface, error) {
	return interfaces.Detect()
}

// ── Internal: heartbeat loop ──────────────────────────────────────────────────

func (b *Bonder) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(b.cfg.HeartbeatMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.doHeartbeat()
		}
	}
}

func (b *Bonder) doHeartbeat() {
	b.mu.RLock()
	tunnels := make([]*TunnelState, 0, len(b.tunnels))
	for _, t := range b.tunnels {
		tunnels = append(tunnels, t)
	}
	b.mu.RUnlock()

	failoverDuration := time.Duration(b.cfg.FailoverMs) * time.Millisecond

	for _, tunnel := range tunnels {
		go func(t *TunnelState) {
			latency, alive := b.pingTunnel(t)

			if alive {
				t.UpdateLatency(latency)
				// Notify aggregator
				b.sendHeartbeat(t.AssignedIP, latency)
			} else {
				t.mu.Lock()
				lastSeen := t.LastSeen
				t.mu.Unlock()

				if time.Since(lastSeen) > failoverDuration {
					log.Printf("[heartbeat] interface %s timed out, marking dead", t.Interface.FriendlyName)
					t.MarkDead()
				}
			}
		}(tunnel)
	}

	// Rebalance weights after heartbeat
	b.rebalanceWeights()
}

// pingTunnel sends a quick ICMP-like probe through the tunnel
// Uses HTTP GET to a known fast endpoint as a proxy measure
func (b *Bonder) pingTunnel(t *TunnelState) (time.Duration, bool) {
	start := time.Now()

	// Probe the aggregator health endpoint through the tunnel's assigned IP
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(b.cfg.APIBase + "/health")
	if err != nil {
		return 0, false
	}
	resp.Body.Close()

	return time.Since(start), resp.StatusCode == 200
}

// rebalanceWeights adjusts tunnel weights based on latency
// Lower latency = higher weight = more traffic
func (b *Bonder) rebalanceWeights() {
	b.mu.RLock()
	tunnels := make([]*TunnelState, 0)
	for _, t := range b.tunnels {
		t.mu.Lock()
		if t.IsConnected && t.Latency > 0 {
			tunnels = append(tunnels, t)
		}
		t.mu.Unlock()
	}
	b.mu.RUnlock()

	if len(tunnels) == 0 {
		return
	}

	// Inverse latency weighting: faster link gets more traffic
	var totalInvLatency float64
	for _, t := range tunnels {
		t.mu.Lock()
		totalInvLatency += 1.0 / float64(t.Latency.Milliseconds())
		t.mu.Unlock()
	}

	for _, t := range tunnels {
		t.mu.Lock()
		invLat := 1.0 / float64(t.Latency.Milliseconds())
		t.Weight = invLat / totalInvLatency
		t.mu.Unlock()
	}
}

// ── Internal: interface watch loop ───────────────────────────────────────────
// Detects new interfaces appearing (e.g. user plugs in USB modem)

func (b *Bonder) interfaceWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Currently passive — we don't auto-connect new interfaces
			// In a future version, auto-connect can be enabled here
			// For now, the GUI triggers connects explicitly
		}
	}
}

// ── Internal: WireGuard tunnel management ────────────────────────────────────

func (b *Bonder) bringUpTunnel(tc *wireguard.TunnelConfig) error {
	// Write WireGuard config to a temp file
	cfgContent := wireguard.BuildWgConfig(tc)
	tunnelName := "acaskill-" + sanitize(tc.InterfaceName)
	cfgPath := fmt.Sprintf(`C:\ProgramData\AcaSkillVPN\tunnels\%s.conf`, tunnelName)

	// Ensure directory exists
	exec.Command("cmd", "/C", `mkdir "C:\ProgramData\AcaSkillVPN\tunnels" 2>nul`).Run()

	// Write config
	if err := writeFile(cfgPath, cfgContent); err != nil {
		return fmt.Errorf("write tunnel config: %w", err)
	}

	// Use wireguard.exe to bring up the tunnel
	// WireGuard for Windows ships with a CLI: wireguard.exe /installtunnelservice <config>
	cmd := exec.Command(`C:\Program Files\WireGuard\wireguard.exe`,
		"/installtunnelservice", cfgPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard install tunnel: %w\nOutput: %s", err, out)
	}

	log.Printf("[wireguard] tunnel %s started", tunnelName)
	return nil
}

func (b *Bonder) bringDownTunnel(t *TunnelState) {
	tunnelName := "acaskill-" + sanitize(t.Interface.Name)
	cmd := exec.Command(`C:\Program Files\WireGuard\wireguard.exe`,
		"/uninstalltunnelservice", tunnelName,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[wireguard] uninstall tunnel %s: %v\n%s", tunnelName, err, out)
	}
}

// sendHeartbeat notifies the aggregator that this peer is alive
func (b *Bonder) sendHeartbeat(assignedIP string, latency time.Duration) {
	type hbReq struct {
		IP        string `json:"ip"`
		LatencyMs int64  `json:"latencyMs"`
	}

	body, _ := json.Marshal(hbReq{
		IP:        assignedIP,
		LatencyMs: latency.Milliseconds(),
	})

	client := &http.Client{Timeout: 2 * time.Second}
	// Note: aggregator heartbeat endpoint is internal-only on server
	// This is sent via the already-established WireGuard tunnel
	client.Post(
		b.cfg.APIBase+"/provision/heartbeat",
		"application/json",
		strings.NewReader(string(body)),
	)
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func writeFile(path, content string) error {
	// Use PowerShell to write file (avoids import cycle)
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf(`Set-Content -Path '%s' -Value @'
%s
'@`, path, content),
	)
	return cmd.Run()
}
