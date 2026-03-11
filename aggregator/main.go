// =============================================================================
// AcaSkill VPN - Bonding Aggregator v2
// True byte-level bonding: receives packets from multiple tunnels per device,
// reorders them by sequence number, and forwards to internet via TUN.
//
// Packet format (client → server):
//   [4]  Magic: 0xACA51337
//   [16] DeviceID (UUID bytes)
//   [8]  SequenceNumber (uint64, per-device counter)
//   [2]  TunnelIndex (uint16: which tunnel sent this)
//   [2]  PayloadLength (uint16)
//   [N]  Payload (IP packet)
//
// Packet format (server → client):
//   [4]  Magic: 0xACA51338
//   [8]  SequenceNumber (uint64, matches original for ACK, or new seq for return)
//   [2]  PayloadLength (uint16)
//   [N]  Payload (IP packet)
// =============================================================================

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	MagicClient = uint32(0xACA51337)
	MagicServer = uint32(0xACA51338)
	HeaderSize  = 4 + 16 + 8 + 2 + 2 // 32 bytes
	RetHeaderSize = 4 + 8 + 2 + 2     // 16 bytes
	MaxPacket   = 65535
	ReorderBuf  = 256  // reorder buffer size per device
	ReorderWait = 20 * time.Millisecond
)

// ── Config ────────────────────────────────────────────────────────────────────

type Config struct {
	HTTPPort  string
	UDPPort   string
	APISecret string
	WGSubnet  string
	TUNName   string
	TUNAddr   string // e.g. "10.9.0.1/24" — aggregator TUN IP
}

func loadConfig() Config {
	return Config{
		HTTPPort:  getEnv("AGG_PORT", "7878"),
		UDPPort:   getEnv("AGG_UDP_PORT", "7979"),
		APISecret: getEnv("API_SECRET", ""),
		WGSubnet:  getEnv("WG_SUBNET", "10.8.0.0/16"),
		TUNName:   getEnv("TUN_NAME", "acaskill-agg"),
		TUNAddr:   getEnv("TUN_ADDR", "10.9.0.1/24"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ── Peer / Device tracking ────────────────────────────────────────────────────

type Peer struct {
	IP       net.IP
	Addr     *net.UDPAddr // for sending return traffic
	DeviceID string
	Label    string
	LastSeen time.Time
	Latency  time.Duration
	Weight   float64
	mu       sync.Mutex
}

type ReorderEntry struct {
	seq     uint64
	payload []byte
	arrived time.Time
}

type Device struct {
	ID          string
	Peers       map[string]*Peer
	nextExpected uint64          // next seq we're waiting to forward
	reorderBuf  [ReorderBuf]*ReorderEntry
	outSeq      atomic.Uint64   // seq counter for return packets
	mu          sync.Mutex
}

func (d *Device) addPeer(p *Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Peers[p.IP.String()] = p
	log.Printf("[device %s] peer added: %s (%s)", d.ID[:8], p.IP, p.Label)
}

func (d *Device) activePeers() []*Peer {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Second)
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

// insertReorder places a packet into the reorder buffer
// Returns slice of in-order payloads ready to forward
func (d *Device) insertReorder(seq uint64, payload []byte) [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()

	// If seq is what we expect, fast path
	if seq == d.nextExpected {
		d.nextExpected++
		out := [][]byte{payload}
		// Drain any buffered consecutive packets
		for {
			slot := d.nextExpected % ReorderBuf
			e := d.reorderBuf[slot]
			if e == nil || e.seq != d.nextExpected {
				break
			}
			out = append(out, e.payload)
			d.reorderBuf[slot] = nil
			d.nextExpected++
		}
		return out
	}

	// Out of order — buffer it if not too old
	if seq > d.nextExpected && seq-d.nextExpected < ReorderBuf {
		slot := seq % ReorderBuf
		d.reorderBuf[slot] = &ReorderEntry{seq: seq, payload: payload, arrived: time.Now()}
	}

	// Flush stale buffered packets (timeout-based)
	cutoff := time.Now().Add(-ReorderWait)
	var out [][]byte
	for {
		slot := d.nextExpected % ReorderBuf
		e := d.reorderBuf[slot]
		if e == nil {
			// Check if we've been waiting too long — skip gap
			// (handled by heartbeat flush, not here)
			break
		}
		if e.seq != d.nextExpected {
			if e.arrived.Before(cutoff) {
				// Stale entry in wrong slot, clear it
				d.reorderBuf[slot] = nil
			}
			break
		}
		out = append(out, e.payload)
		d.reorderBuf[slot] = nil
		d.nextExpected++
	}
	return out
}

// flushStale forces any buffered packets older than ReorderWait to be forwarded
func (d *Device) flushStale() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-ReorderWait)
	var out [][]byte
	for {
		slot := d.nextExpected % ReorderBuf
		e := d.reorderBuf[slot]
		if e == nil {
			break
		}
		if e.arrived.After(cutoff) {
			break // not stale yet
		}
		// Skip gap — forward what we have
		out = append(out, e.payload)
		d.reorderBuf[slot] = nil
		d.nextExpected = e.seq + 1
	}
	return out
}

// ── Aggregator ────────────────────────────────────────────────────────────────

type Aggregator struct {
	cfg     Config
	devices sync.Map // deviceID -> *Device
	ipIndex sync.Map // ipStr -> deviceID
	udpConn *net.UDPConn
	tunFd   *os.File // TUN file descriptor for forwarding
	tunW    chan []byte // packets to write to TUN
}

func NewAggregator(cfg Config) *Aggregator {
	return &Aggregator{
		cfg:  cfg,
		tunW: make(chan []byte, 4096),
	}
}

func (a *Aggregator) getOrCreateDevice(deviceID string) *Device {
	dev, _ := a.devices.LoadOrStore(deviceID, &Device{
		ID:    deviceID,
		Peers: make(map[string]*Peer),
	})
	return dev.(*Device)
}

// ── UDP Data Plane ────────────────────────────────────────────────────────────

func (a *Aggregator) startUDP(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", ":"+a.cfg.UDPPort)
	if err != nil {
		return fmt.Errorf("resolve UDP: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}
	a.udpConn = conn
	log.Printf("[udp] bonding data plane listening on :%s", a.cfg.UDPPort)

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	go a.udpReadLoop(conn)
	go a.flushLoop(ctx)
	return nil
}

func (a *Aggregator) udpReadLoop(conn *net.UDPConn) {
	buf := make([]byte, MaxPacket+HeaderSize)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("[udp] read error: %v", err)
			return
		}
		if n < HeaderSize {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go a.handlePacket(pkt, addr)
	}
}

func (a *Aggregator) handlePacket(pkt []byte, addr *net.UDPAddr) {
	// Parse header
	magic := binary.BigEndian.Uint32(pkt[0:4])
	if magic != MagicClient {
		return
	}

	deviceIDBytes := pkt[4:20]
	deviceID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		deviceIDBytes[0:4], deviceIDBytes[4:6], deviceIDBytes[6:8],
		deviceIDBytes[8:10], deviceIDBytes[10:16])

	seq := binary.BigEndian.Uint64(pkt[20:28])
	tunnelIdx := binary.BigEndian.Uint16(pkt[28:30])
	payloadLen := binary.BigEndian.Uint16(pkt[30:32])

	if int(payloadLen)+HeaderSize > len(pkt) {
		return
	}
	payload := pkt[HeaderSize : HeaderSize+int(payloadLen)]

	// Update peer last-seen
	dev := a.getOrCreateDevice(deviceID)
	srcIP := addr.IP.String()

	dev.mu.Lock()
	peer, exists := dev.Peers[srcIP]
	if !exists {
		peer = &Peer{
			IP:       addr.IP,
			Addr:     addr,
			DeviceID: deviceID,
			Label:    fmt.Sprintf("tunnel-%d", tunnelIdx),
			Weight:   1.0,
		}
		dev.Peers[srcIP] = peer
		a.ipIndex.Store(srcIP, deviceID)
		log.Printf("[udp] new peer %s for device %s", srcIP, deviceID[:8])
	}
	peer.mu.Lock()
	peer.LastSeen = time.Now()
	peer.Addr = addr
	peer.mu.Unlock()
	dev.mu.Unlock()

	// Reorder and forward
	ready := dev.insertReorder(seq, payload)
	for _, p := range ready {
		a.forwardToInternet(p)
	}
}

func (a *Aggregator) forwardToInternet(payload []byte) {
	if len(payload) == 0 {
		return
	}
	select {
	case a.tunW <- payload:
	default:
		// Drop if buffer full
	}
}

// ── TUN write loop ────────────────────────────────────────────────────────────

func (a *Aggregator) tunWriteLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkt := <-a.tunW:
			if a.tunFd != nil {
				a.tunFd.Write(pkt)
			}
		}
	}
}

// ── Return traffic: TUN → split across client tunnels ─────────────────────────

func (a *Aggregator) tunReadLoop(ctx context.Context) {
	if a.tunFd == nil {
		return
	}
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := a.tunFd.Read(buf)
		if err != nil {
			continue
		}
		if n < 20 {
			continue
		}
		// Extract destination IP from IP header
		dstIP := net.IP(buf[16:20]).String()
		devIDRaw, ok := a.ipIndex.Load(dstIP)
		if !ok {
			continue
		}
		devRaw, ok := a.devices.Load(devIDRaw.(string))
		if !ok {
			continue
		}
		dev := devRaw.(*Device)
		peers := dev.activePeers()
		if len(peers) == 0 {
			continue
		}
		// Round-robin return traffic across all active tunnels
		seq := dev.outSeq.Add(1)
		peerIdx := int(seq) % len(peers)
		peer := peers[peerIdx]

		// Build return packet
		payload := buf[:n]
		retPkt := make([]byte, RetHeaderSize+len(payload))
		binary.BigEndian.PutUint32(retPkt[0:4], MagicServer)
		binary.BigEndian.PutUint64(retPkt[4:12], seq)
		binary.BigEndian.PutUint16(retPkt[12:14], uint16(len(payload)))
		binary.BigEndian.PutUint16(retPkt[14:16], 0) // reserved
		copy(retPkt[RetHeaderSize:], payload)

		peer.mu.Lock()
		peerAddr := peer.Addr
		peer.mu.Unlock()
		if peerAddr != nil {
			a.udpConn.WriteToUDP(retPkt, peerAddr)
		}
	}
}

// ── Stale flush loop ──────────────────────────────────────────────────────────

func (a *Aggregator) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(ReorderWait / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.devices.Range(func(_, val interface{}) bool {
				dev := val.(*Device)
				for _, p := range dev.flushStale() {
					a.forwardToInternet(p)
				}
				return true
			})
		}
	}
}

// ── TUN setup ─────────────────────────────────────────────────────────────────

func (a *Aggregator) setupTUN() error {
	// Create TUN interface
	out, err := exec.Command("ip", "tuntap", "add", "dev", a.cfg.TUNName, "mode", "tun").CombinedOutput()
	if err != nil {
		log.Printf("[tun] create warning (may already exist): %s", string(out))
	}
	// Assign IP
	exec.Command("ip", "addr", "add", a.cfg.TUNAddr, "dev", a.cfg.TUNName).Run()
	// Bring up
	exec.Command("ip", "link", "set", a.cfg.TUNName, "up").Run()
	// Route WG subnet through TUN for return traffic
	exec.Command("ip", "route", "add", a.cfg.WGSubnet, "dev", a.cfg.TUNName).Run()

	// Open TUN fd
	fd, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/net/tun: %w", err)
	}
	// ioctl to bind to our interface name — simplified: use fd directly
	// In production use golang.zx2c4.com/wireguard/tun
	a.tunFd = fd
	log.Printf("[tun] interface %s up with %s", a.cfg.TUNName, a.cfg.TUNAddr)
	return nil
}

// ── HTTP control API ──────────────────────────────────────────────────────────

type RegisterRequest struct {
	DeviceID string `json:"deviceId"`
	IP       string `json:"ip"`
	Label    string `json:"label"`
}

type HeartbeatRequest struct {
	IP        string `json:"ip"`
	LatencyMs int64  `json:"latencyMs"`
}

type StatsResponse struct {
	Devices int          `json:"devices"`
	Peers   int          `json:"peers"`
	Detail  []DeviceStat `json:"detail"`
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

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Secret") != a.cfg.APISecret {
				http.Error(w, "Unauthorized", 401)
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/register", auth(func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		json.NewDecoder(r.Body).Decode(&req)
		dev := a.getOrCreateDevice(req.DeviceID)
		ip := net.ParseIP(req.IP)
		if ip == nil {
			http.Error(w, "bad ip", 400); return
		}
		p := &Peer{IP: ip, DeviceID: req.DeviceID, Label: req.Label, LastSeen: time.Now(), Weight: 1.0}
		dev.addPeer(p)
		a.ipIndex.Store(req.IP, req.DeviceID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/heartbeat", auth(func(w http.ResponseWriter, r *http.Request) {
		var req HeartbeatRequest
		json.NewDecoder(r.Body).Decode(&req)
		devIDRaw, ok := a.ipIndex.Load(req.IP)
		if ok {
			devRaw, ok := a.devices.Load(devIDRaw.(string))
			if ok {
				dev := devRaw.(*Device)
				dev.mu.Lock()
				if p, ok := dev.Peers[req.IP]; ok {
					p.mu.Lock()
					p.LastSeen = time.Now()
					p.Latency = time.Duration(req.LatencyMs) * time.Millisecond
					p.mu.Unlock()
				}
				dev.mu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/stats", auth(func(w http.ResponseWriter, r *http.Request) {
		stats := StatsResponse{}
		a.devices.Range(func(_, val interface{}) bool {
			dev := val.(*Device)
			active := dev.activePeers()
			ds := DeviceStat{DeviceID: dev.ID, ActivePeers: len(active)}
			for _, p := range active {
				p.mu.Lock()
				ds.Peers = append(ds.Peers, PeerStat{
					IP: p.IP.String(), Label: p.Label,
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
		fmt.Fprintf(w, `{"ok":true,"udp":%q}`, ":"+a.cfg.UDPPort)
	})

	addr := ":" + a.cfg.HTTPPort
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("[http] control API on %s", addr)
		srv.ListenAndServe()
	}()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}

// ── Cleanup ───────────────────────────────────────────────────────────────────

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
						log.Printf("[cleanup] removed stale peer %s", ip)
					}
				}
				empty := len(dev.Peers) == 0
				dev.mu.Unlock()
				if empty {
					a.devices.Delete(key)
				}
				return true
			})
		}
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	if cfg.APISecret == "" {
		log.Fatal("API_SECRET required")
	}

	agg := NewAggregator(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	// Setup TUN (best effort — may not work in all container environments)
	if err := agg.setupTUN(); err != nil {
		log.Printf("[tun] WARNING: TUN setup failed: %v (running in proxy-only mode)", err)
	}

	// Start UDP data plane
	if err := agg.startUDP(ctx); err != nil {
		log.Fatalf("UDP start failed: %v", err)
	}

	go agg.cleanupLoop(ctx)
	go agg.tunWriteLoop(ctx)
	go agg.tunReadLoop(ctx)
	go agg.startHTTP(ctx)

	log.Printf("AcaSkill VPN Aggregator v2 started (HTTP :%s, UDP :%s)",
		cfg.HTTPPort, cfg.UDPPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	log.Println("Shutting down...")
	cancel()
	time.Sleep(2 * time.Second)
}
