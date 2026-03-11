// internal/tun/adapter.go
// TUN-based bonding adapter for AcaSkill VPN
// Creates a virtual NIC (acaskill-bond0), intercepts all IP traffic,
// splits packets across multiple WireGuard tunnels at the packet level,
// and reassembles return traffic back into the TUN.

package tun

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	wtun "golang.zx2c4.com/wireguard/tun"
)

// ── Protocol constants (must match aggregator/main.go) ───────────────────────

const (
	MagicClient   = uint32(0xACA51337)
	MagicServer   = uint32(0xACA51338)
	HeaderSize    = 32 // 4+16+8+2+2
	RetHeaderSize = 16 // 4+8+2+2
	MaxPacket     = 65535

	AdapterName = "acaskill-bond0"
	AdapterAddr = "10.99.0.2"
	AdapterMask = "255.255.255.0"
	AdapterGW   = "10.99.0.1"
	AdapterMTU  = 1420
)

// ── Endpoint ──────────────────────────────────────────────────────────────────

// Endpoint is one WireGuard tunnel used for bonding traffic.
type Endpoint struct {
	Label      string
	TunnelIP   string       // this device's assigned IP in this WG tunnel
	ServerAddr *net.UDPAddr // aggregator UDP address
	conn       *net.UDPConn // UDP socket bound to TunnelIP
	active     bool
	lastSeen   time.Time
	mu         sync.Mutex
}

func (e *Endpoint) send(pkt []byte) {
	e.mu.Lock()
	conn := e.conn
	addr := e.ServerAddr
	e.mu.Unlock()
	if conn != nil && addr != nil {
		conn.WriteToUDP(pkt, addr)
	}
}

// ── Adapter ───────────────────────────────────────────────────────────────────

type Adapter struct {
	deviceID  [16]byte
	dev       wtun.Device
	endpoints []*Endpoint
	epMu      sync.RWMutex
	txSeq     atomic.Uint64
	retBuf    chan []byte
	stopOnce  sync.Once
	stopCh    chan struct{}
}

// New creates a bonding adapter for the given device UUID string.
func New(deviceIDStr string) (*Adapter, error) {
	id, err := parseUUID(deviceIDStr)
	if err != nil {
		return nil, fmt.Errorf("parseUUID: %w", err)
	}
	return &Adapter{
		deviceID: id,
		retBuf:   make(chan []byte, 8192),
		stopCh:   make(chan struct{}),
	}, nil
}

// AddEndpoint registers a WireGuard tunnel as a bonding path.
// Call this after each tunnel is connected, before Start().
// tunnelIP: device's assigned IP in this tunnel (e.g. "10.8.1.4")
// serverHost: aggregator host (e.g. "vpn.acaskill.com")
// serverPort: aggregator UDP port (e.g. 7979)
func (a *Adapter) AddEndpoint(label, physicalIP, tunnelIP, serverHost string, serverPort int) error {
	serverAddr, err := net.ResolveUDPAddr("udp4",
		fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		return fmt.Errorf("resolve server: %w", err)
	}

	// Bind to the tunnel IP so traffic routes through that WG interface
	localAddr, err := net.ResolveUDPAddr("udp4", physicalIP+":0")
	if err != nil {
		return fmt.Errorf("resolve local: %w", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", tunnelIP, err)
	}

	ep := &Endpoint{
		Label:      label,
		TunnelIP:   tunnelIP,
		ServerAddr: serverAddr,
		conn:       conn,
		active:     true,
		lastSeen:   time.Now(),
	}

	a.epMu.Lock()
	a.endpoints = append(a.endpoints, ep)
	a.epMu.Unlock()

	log.Printf("[bond] endpoint %s: %s → %s:%d", label, tunnelIP, serverHost, serverPort)
	return nil
}

// RemoveEndpoint deactivates a tunnel endpoint by label.
func (a *Adapter) RemoveEndpoint(label string) {
	a.epMu.Lock()
	defer a.epMu.Unlock()
	for _, ep := range a.endpoints {
		ep.mu.Lock()
		if ep.Label == label {
			ep.active = false
			if ep.conn != nil {
				ep.conn.Close()
				ep.conn = nil
			}
			log.Printf("[bond] endpoint removed: %s", label)
		}
		ep.mu.Unlock()
	}
}

// Start creates the TUN device and starts packet forwarding.
func (a *Adapter) Start(ctx context.Context) error {
	dev, err := wtun.CreateTUN(AdapterName, AdapterMTU)
	if err != nil {
		return fmt.Errorf("CreateTUN: %w", err)
	}
	a.dev = dev

	// Configure IP address on the new adapter
	if err := a.configure(); err != nil {
		dev.Close()
		return fmt.Errorf("configure: %w", err)
	}

	// Start receive loops for each endpoint
	a.epMu.RLock()
	for _, ep := range a.endpoints {
		go a.recvLoop(ctx, ep)
	}
	a.epMu.RUnlock()

	go a.txLoop(ctx)
	go a.rxLoop(ctx)

	log.Printf("[bond] TUN adapter %s started (%s)", AdapterName, AdapterAddr)
	return nil
}

// Stop shuts down the adapter.
func (a *Adapter) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		if a.dev != nil {
			a.dev.Close()
		}
		a.epMu.Lock()
		for _, ep := range a.endpoints {
			ep.mu.Lock()
			ep.active = false
			if ep.conn != nil {
				ep.conn.Close()
				ep.conn = nil
			}
			ep.mu.Unlock()
		}
		a.epMu.Unlock()
		// Remove default route
		exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0", AdapterGW).Run()
		log.Printf("[bond] adapter stopped")
	})
}

// ── TX loop: TUN → split across endpoints ────────────────────────────────────

func (a *Adapter) txLoop(ctx context.Context) {
	bufs := make([][]byte, 1)
	sizes := make([]int, 1)
	bufs[0] = make([]byte, MaxPacket)

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		default:
		}

		n, err := a.dev.Read(bufs, sizes, 0)
		if err != nil || n == 0 {
			select {
			case <-a.stopCh:
				return
			default:
				continue
			}
		}

		payload := make([]byte, sizes[0])
		copy(payload, bufs[0][:sizes[0]])

		eps := a.active()
		if len(eps) == 0 {
			continue
		}

		seq := a.txSeq.Add(1) - 1
		ep := eps[int(seq)%len(eps)]
		a.sendWrapped(ep, seq, payload)
	}
}

func (a *Adapter) sendWrapped(ep *Endpoint, seq uint64, payload []byte) {
	pkt := make([]byte, HeaderSize+len(payload))
	binary.BigEndian.PutUint32(pkt[0:4], MagicClient)
	copy(pkt[4:20], a.deviceID[:])
	binary.BigEndian.PutUint64(pkt[20:28], seq)

	// Find tunnel index
	a.epMu.RLock()
	tunIdx := uint16(0)
	for i, e := range a.endpoints {
		if e == ep {
			tunIdx = uint16(i)
			break
		}
	}
	a.epMu.RUnlock()

	binary.BigEndian.PutUint16(pkt[28:30], tunIdx)
	binary.BigEndian.PutUint16(pkt[30:32], uint16(len(payload)))
	copy(pkt[HeaderSize:], payload)
	ep.send(pkt)
}

// ── RX loop: inject return packets into TUN ───────────────────────────────────

func (a *Adapter) rxLoop(ctx context.Context) {
	bufs := make([][]byte, 1)
	sizes := make([]int, 1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case pkt := <-a.retBuf:
			bufs[0] = pkt
			sizes[0] = len(pkt)
			a.dev.Write(bufs, 0)
		}
	}
}

// ── Per-endpoint receive loop ─────────────────────────────────────────────────

func (a *Adapter) recvLoop(ctx context.Context, ep *Endpoint) {
	buf := make([]byte, MaxPacket+RetHeaderSize)
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		default:
		}

		ep.mu.Lock()
		conn := ep.conn
		ep.mu.Unlock()
		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if n < RetHeaderSize {
			continue
		}

		magic := binary.BigEndian.Uint32(buf[0:4])
		if magic != MagicServer {
			continue
		}
		payloadLen := int(binary.BigEndian.Uint16(buf[12:14]))
		if RetHeaderSize+payloadLen > n {
			continue
		}

		pkt := make([]byte, payloadLen)
		copy(pkt, buf[RetHeaderSize:RetHeaderSize+payloadLen])

		ep.mu.Lock()
		ep.lastSeen = time.Now()
		ep.mu.Unlock()

		select {
		case a.retBuf <- pkt:
		default:
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (a *Adapter) active() []*Endpoint {
	a.epMu.RLock()
	defer a.epMu.RUnlock()
	var out []*Endpoint
	for _, ep := range a.endpoints {
		ep.mu.Lock()
		ok := ep.active
		ep.mu.Unlock()
		if ok {
			out = append(out, ep)
		}
	}
	return out
}

func (a *Adapter) configure() error {
	// Set static IP on the TUN adapter
	if err := netsh("interface", "ip", "set", "address",
		"name="+AdapterName, "static", AdapterAddr, AdapterMask, AdapterGW); err != nil {
		log.Printf("[bond] netsh addr warning: %v", err)
	}
	// Set low metric so it's preferred
	if err := netsh("interface", "ip", "set", "interface",
		AdapterName, "metric=1"); err != nil {
		log.Printf("[bond] netsh metric warning: %v", err)
	}
	// Add default route through our gateway
	exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0",
		AdapterGW, "metric", "1").Run()
	return nil
}

func netsh(args ...string) error {
	out, err := exec.Command("netsh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %v: %w: %s", args, err, string(out))
	}
	return nil
}

func parseUUID(s string) ([16]byte, error) {
	var id [16]byte
	clean := ""
	for _, c := range s {
		if c != '-' {
			clean += string(c)
		}
	}
	if len(clean) != 32 {
		return id, fmt.Errorf("invalid UUID: %q", s)
	}
	for i := 0; i < 16; i++ {
		var v byte
		fmt.Sscanf(clean[i*2:i*2+2], "%02x", &v)
		id[i] = v
	}
	return id, nil
}
