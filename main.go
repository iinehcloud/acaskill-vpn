// =============================================================================
// AcaSkill VPN - Bonding Aggregator (Server Side)
// Accepts multiple WireGuard tunnel connections from the same client device,
// aggregates them into a single logical stream, and forwards traffic.
//
// How it works:
//   - Each client interface (MTN, Glo, WiFi etc.) has its own WireGuard tunnel
//     with its own assigned IP in 10.8.0.0/16
//   - The aggregator tracks which IPs belong to the same device
//   - Packets from all those IPs are combined and forwarded to the internet
//   - Return traffic is distributed back across all active tunnels
// =============================================================================

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ── Config ────────────────────────────────────────────────────────────────────

type Config struct {
	AggPort   string
	APIURL    string
	APISecret string
	WGSubnet  string
}

func loadConfig() Config {
	return Config{
		AggPort:   getEnv("AGG_PORT", "7878"),
		APIURL:    getEnv("API_URL", "http://api:3000"),
		APISecret: getEnv("API_SECRET", ""),
		WGSubnet:  getEnv("WG_SUBNET", "10.8.0.0/16"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ── Peer / Device tracking ────────────────────────────────────────────────────

// Peer represents one WireGuard tunnel (one interface on the client)
type Peer struct {
	IP           net.IP
	DeviceID     string
	Label        string        // "MTN", "Glo", etc.
	LastSeen     time.Time
	Latency      time.Duration
	BytesSent    uint64
	BytesRecv    uint64
	Weight       float64       // 0.0-1.0, updated by link quality
	mu           sync.Mutex
}

// Device groups all peers (interfaces) for one client device
type Device struct {
	ID      string
	Peers   map[string]*Peer // keyed by IP string
	mu      sync.RWMutex
}

func (d *Device) addPeer(p *Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Peers[p.IP.String()] = p
	log.Printf("[device %s] peer added: %s (%s)", d.ID, p.IP, p.Label)
}

func (d *Device) removePeer(ip string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.Peers, ip)
	log.Printf("[device %s] peer removed: %s", d.ID, ip)
}

func (d *Device) activePeers() []*Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	cutoff := time.Now().Add(-3 * time.Second)
	var out []*Peer
	for _, p := range d.Peers {
		p.mu.Lock()
		alive := p.LastSeen.After(cutoff)
		p.mu.Unlock()
		if alive {
			out = append(out, p)
		}
	}
	return out
}

// ── Aggregator ────────────────────────────────────────────────────────────────

type Aggregator struct {
	cfg     Config
	devices sync.Map // deviceID -> *Device
	ipIndex sync.Map // ipStr -> deviceID (for reverse lookup)
}

func NewAggregator(cfg Config) *Aggregator {
	return &Aggregator{cfg: cfg}
}

// RegisterPeer maps an IP to a device. Called when a peer connects.
func (a *Aggregator) RegisterPeer(deviceID, ipStr, label string) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		log.Printf("invalid IP: %s", ipStr)
		return
	}

	peer := &Peer{
		IP:       ip,
		DeviceID: deviceID,
		Label:    label,
		LastSeen: time.Now(),
		Weight:   1.0,
	}

	dev, _ := a.devices.LoadOrStore(deviceID, &Device{
		ID:    deviceID,
		Peers: make(map[string]*Peer),
	})
	dev.(*Device).addPeer(peer)
	a.ipIndex.Store(ipStr, deviceID)
}

// HeartbeatPeer updates last-seen timestamp and latency for a peer
func (a *Aggregator) HeartbeatPeer(ipStr string, latency time.Duration) {
	devIDRaw, ok := a.ipIndex.Load(ipStr)
	if !ok {
		return
	}
	devRaw, ok := a.devices.Load(devIDRaw.(string))
	if !ok {
		return
	}
	dev := devRaw.(*Device)
	dev.mu.RLock()
	p, ok := dev.Peers[ipStr]
	dev.mu.RUnlock()
	if ok {
		p.mu.Lock()
		p.LastSeen = time.Now()
		p.Latency  = latency
		p.mu.Unlock()
	}
}

// ChoosePeer picks the best outbound peer for a device using weighted random
// Currently: pick peer with lowest latency among active peers.
// In Phase 2, this becomes weighted round-robin with per-packet distribution.
func (a *Aggregator) ChoosePeer(deviceID string) *Peer {
	devRaw, ok := a.devices.Load(deviceID)
	if !ok {
		return nil
	}
	peers := devRaw.(*Device).activePeers()
	if len(peers) == 0 {
		return nil
	}

	best := peers[0]
	for _, p := range peers[1:] {
		p.mu.Lock()
		bestLat := best.Latency
		pLat    := p.Latency
		p.mu.Unlock()

		if pLat > 0 && (bestLat == 0 || pLat < bestLat) {
			best = p
		}
	}
	return best
}

// ── HTTP control API (used internally by provisioning API) ───────────────────

type RegisterRequest struct {
	DeviceID string `json:"deviceId"`
	IP       string `json:"ip"`
	Label    string `json:"label"`
}

type HeartbeatRequest struct {
	IP      string `json:"ip"`
	LatencyMs int64 `json:"latencyMs"`
}

type StatsResponse struct {
	Devices int            `json:"devices"`
	Peers   int            `json:"peers"`
	Detail  []DeviceStat   `json:"detail"`
}

type DeviceStat struct {
	DeviceID    string     `json:"deviceId"`
	ActivePeers int        `json:"activePeers"`
	Peers       []PeerStat `json:"peers"`
}

type PeerStat struct {
	IP        string  `json:"ip"`
	Label     string  `json:"label"`
	LatencyMs float64 `json:"latencyMs"`
	LastSeen  string  `json:"lastSeen"`
}

func (a *Aggregator) startHTTP(ctx context.Context) {
	mux := http.NewServeMux()

	// Auth middleware
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Secret") != a.cfg.APISecret {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/register", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405); return
		}
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400); return
		}
		a.RegisterPeer(req.DeviceID, req.IP, req.Label)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/heartbeat", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405); return
		}
		var req HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400); return
		}
		a.HeartbeatPeer(req.IP, time.Duration(req.LatencyMs)*time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/stats", auth(func(w http.ResponseWriter, r *http.Request) {
		stats := StatsResponse{}
		a.devices.Range(func(key, val interface{}) bool {
			dev := val.(*Device)
			active := dev.activePeers()
			ds := DeviceStat{
				DeviceID:    dev.ID,
				ActivePeers: len(active),
			}
			for _, p := range active {
				p.mu.Lock()
				ds.Peers = append(ds.Peers, PeerStat{
					IP:        p.IP.String(),
					Label:     p.Label,
					LatencyMs: float64(p.Latency.Milliseconds()),
					LastSeen:  p.LastSeen.Format(time.RFC3339),
				})
				p.mu.Unlock()
			}
			stats.Devices++
			stats.Peers += len(active)
			stats.Detail = append(stats.Detail, ds)
			return true
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true}`)
	})

	addr := ":" + a.cfg.AggPort
	srv  := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("Aggregator HTTP listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("aggregator HTTP error: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}

// ── Stale peer cleanup ────────────────────────────────────────────────────────

func (a *Aggregator) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-60 * time.Second)
			a.devices.Range(func(key, val interface{}) bool {
				dev := val.(*Device)
				dev.mu.Lock()
				for ip, p := range dev.Peers {
					p.mu.Lock()
					stale := p.LastSeen.Before(cutoff)
					p.mu.Unlock()
					if stale {
						delete(dev.Peers, ip)
						a.ipIndex.Delete(ip)
						log.Printf("[cleanup] removed stale peer %s from device %s", ip, dev.ID)
					}
				}
				if len(dev.Peers) == 0 {
					a.devices.Delete(key)
					log.Printf("[cleanup] removed empty device %s", dev.ID)
				}
				dev.mu.Unlock()
				return true
			})
		}
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	if cfg.APISecret == "" {
		log.Fatal("API_SECRET environment variable required")
	}

	agg := NewAggregator(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	go agg.cleanupLoop(ctx)
	go agg.startHTTP(ctx)

	log.Printf("AcaSkill VPN Aggregator started (port %s)", cfg.AggPort)

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	log.Println("Shutting down aggregator...")
	cancel()
	time.Sleep(2 * time.Second)
	log.Println("Aggregator stopped")
}
