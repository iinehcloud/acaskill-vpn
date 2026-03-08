// =============================================================================
// AcaSkill VPN - Bonding Proxy
// A SOCKS5 proxy that round-robins outgoing TCP connections across all active
// WireGuard tunnel interfaces, achieving real per-connection load balancing.
// =============================================================================

package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TunnelIface represents one active WireGuard tunnel interface
type TunnelIface struct {
	Name      string // e.g. "acaskill-wi-fi"
	AssignedIP string // e.g. "10.8.1.2"
}

// Proxy is a SOCKS5 proxy that bonds multiple tunnel interfaces
type Proxy struct {
	listenAddr string
	mu         sync.RWMutex
	tunnels    []TunnelIface
	counter    atomic.Uint64
	listener   net.Listener
}

func New(listenAddr string) *Proxy {
	return &Proxy{listenAddr: listenAddr}
}

// UpdateTunnels refreshes the list of active tunnel interfaces
func (p *Proxy) UpdateTunnels(tunnels []TunnelIface) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tunnels = make([]TunnelIface, len(tunnels))
	copy(p.tunnels, tunnels)
	log.Printf("[proxy] updated tunnels: %d active", len(tunnels))
	for _, t := range tunnels {
		log.Printf("[proxy]   %s -> %s", t.Name, t.AssignedIP)
	}
}

// pickTunnel returns the next tunnel IP to use (round-robin)
func (p *Proxy) pickTunnel() (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.tunnels) == 0 {
		return "", false
	}
	idx := p.counter.Add(1) - 1
	t := p.tunnels[idx%uint64(len(p.tunnels))]
	return t.AssignedIP, true
}

// Start begins the SOCKS5 listener
func (p *Proxy) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("proxy listen on %s: %w", p.listenAddr, err)
	}
	p.listener = ln
	log.Printf("[proxy] SOCKS5 bonding proxy listening on %s", p.listenAddr)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("[proxy] accept error: %v", err)
					continue
				}
			}
			go p.handleConn(conn)
		}
	}()
	return nil
}

func (p *Proxy) handleConn(client net.Conn) {
	defer client.Close()
	client.SetDeadline(time.Now().Add(30 * time.Second))

	// ── SOCKS5 handshake ──────────────────────────────────────
	// 1. Read greeting: VER NMETHODS METHODS...
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return // not SOCKS5
	}
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	// Reply: no auth required
	client.Write([]byte{0x05, 0x00})

	// 2. Read request: VER CMD RSV ATYP DST.ADDR DST.PORT
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		return
	}
	if header[0] != 0x05 || header[1] != 0x01 { // only CONNECT supported
		client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // command not supported
		return
	}

	var dstAddr string
	switch header[3] {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(client, addr); err != nil {
			return
		}
		dstAddr = net.IP(addr).String()
	case 0x03: // domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(client, lenBuf); err != nil {
			return
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(client, domain); err != nil {
			return
		}
		dstAddr = string(domain)
	case 0x04: // IPv6
		addr := make([]byte, 16)
		if _, err := io.ReadFull(client, addr); err != nil {
			return
		}
		dstAddr = net.IP(addr).String()
	default:
		client.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(client, portBuf); err != nil {
		return
	}
	dstPort := binary.BigEndian.Uint16(portBuf)
	target := fmt.Sprintf("%s:%d", dstAddr, dstPort)

	// ── Pick tunnel interface and dial ────────────────────────
	tunnelIP, ok := p.pickTunnel()
	if !ok {
		// No tunnels active — connect directly (fallback)
		tunnelIP = ""
	}

	var dialer net.Dialer
	if tunnelIP != "" {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(tunnelIP)}
	}

	server, err := dialer.DialContext(context.Background(), "tcp", target)
	if err != nil {
		log.Printf("[proxy] dial %s via %s failed: %v", target, tunnelIP, err)
		client.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // host unreachable
		return
	}
	defer server.Close()

	// Send success response
	localAddr := server.LocalAddr().(*net.TCPAddr)
	resp := []byte{0x05, 0x00, 0x00, 0x01}
	resp = append(resp, localAddr.IP.To4()...)
	resp = append(resp, byte(localAddr.Port>>8), byte(localAddr.Port))
	client.Write(resp)

	log.Printf("[proxy] %s -> %s (via %s)", client.RemoteAddr(), target, tunnelIP)

	// ── Bidirectional pipe ────────────────────────────────────
	client.SetDeadline(time.Time{}) // remove deadline
	server.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() { io.Copy(server, client); done <- struct{}{} }()
	go func() { io.Copy(client, server); done <- struct{}{} }()
	<-done
}
