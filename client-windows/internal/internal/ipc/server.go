// =============================================================================
// AcaSkill VPN - IPC Server
// Named pipe server that lets the GUI talk to the daemon.
// Uses JSON messages over \\.\pipe\acaskill-vpn
// =============================================================================

package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/acaskill/vpn-client/internal/bonding"
	"github.com/acaskill/vpn-client/internal/interfaces"
)

// ── Message types ─────────────────────────────────────────────────────────────

type MessageType string

const (
	// GUI → Daemon
	MsgGetStatus           MessageType = "GET_STATUS"
	MsgGetInterfaces       MessageType = "GET_INTERFACES"
	MsgConnectInterface    MessageType = "CONNECT_INTERFACE"
	MsgDisconnectInterface MessageType = "DISCONNECT_INTERFACE"
	MsgConnectAll          MessageType = "CONNECT_ALL"
	MsgDisconnectAll       MessageType = "DISCONNECT_ALL"
	MsgSetLicense          MessageType = "SET_LICENSE"

	// Daemon → GUI (responses)
	MsgStatus     MessageType = "STATUS"
	MsgInterfaces MessageType = "INTERFACES"
	MsgOK         MessageType = "OK"
	MsgError      MessageType = "ERROR"
)

type Request struct {
	ID      string          `json:"id"`
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	ID      string          `json:"id"`
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type ConnectInterfacePayload struct {
	InterfaceName string `json:"interfaceName"`
}

type SetLicensePayload struct {
	LicenseKey string `json:"licenseKey"`
	DeviceName string `json:"deviceName"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}

// ── Server ────────────────────────────────────────────────────────────────────

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Start(ctx context.Context, bonder *bonding.Bonder) error {
	// Use TCP on localhost as a cross-platform alternative to named pipes
	// Named pipes require extra Windows-specific dependencies
	// GUI connects to localhost:47821
	listener, err := net.Listen("tcp", "127.0.0.1:47821")
	if err != nil {
		return fmt.Errorf("IPC listen: %w", err)
	}

	log.Println("[ipc] server listening on 127.0.0.1:47821")

	go func() {
		defer listener.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Printf("[ipc] accept error: %v", err)
				continue
			}

			go s.handleConn(conn, bonder)
		}
	}()

	return nil
}

func (s *Server) handleConn(conn net.Conn, bonder *bonding.Bonder) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			if err != io.EOF {
				log.Printf("[ipc] decode error: %v", err)
			}
			return
		}

		conn.SetDeadline(time.Now().Add(30 * time.Second))

		resp := s.handle(req, bonder)
		if err := encoder.Encode(resp); err != nil {
			log.Printf("[ipc] encode error: %v", err)
			return
		}
	}
}

func (s *Server) handle(req Request, bonder *bonding.Bonder) Response {
	switch req.Type {

	case MsgGetStatus:
		status := bonder.GetStatus()
		payload, _ := json.Marshal(status)
		return Response{ID: req.ID, Type: MsgStatus, Payload: payload}

	case MsgGetInterfaces:
		ifaces, err := bonder.GetAvailableInterfaces()
		if err != nil {
			return errResp(req.ID, err.Error())
		}
		// Enrich with current connection state
		type ifaceWithState struct {
			interfaces.NetworkInterface
			IsActive bool `json:"isActive"`
		}
		status := bonder.GetStatus()
		activeMap := map[string]bool{}
		for _, t := range status.Tunnels {
			if t.IsConnected {
				activeMap[t.InterfaceName] = true
			}
		}
		enriched := make([]ifaceWithState, len(ifaces))
		for i, iface := range ifaces {
			enriched[i] = ifaceWithState{
				NetworkInterface: iface,
				IsActive:         activeMap[iface.FriendlyName],
			}
		}
		payload, _ := json.Marshal(enriched)
		return Response{ID: req.ID, Type: MsgInterfaces, Payload: payload}

	case MsgConnectInterface:
		var p ConnectInterfacePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errResp(req.ID, "invalid payload")
		}
		ifaces, _ := bonder.GetAvailableInterfaces()
		for _, iface := range ifaces {
			if iface.Name == p.InterfaceName || iface.FriendlyName == p.InterfaceName {
				if err := bonder.ConnectInterface(iface); err != nil {
					return errResp(req.ID, err.Error())
				}
				return okResp(req.ID)
			}
		}
		return errResp(req.ID, "interface not found: "+p.InterfaceName)

	case MsgDisconnectInterface:
		var p ConnectInterfacePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errResp(req.ID, "invalid payload")
		}
		if err := bonder.DisconnectInterface(p.InterfaceName); err != nil {
			return errResp(req.ID, err.Error())
		}
		return okResp(req.ID)

	case MsgConnectAll:
		ifaces, err := bonder.GetAvailableInterfaces()
		if err != nil {
			return errResp(req.ID, err.Error())
		}
		var errors []string
		for _, iface := range ifaces {
			if iface.IsConnected {
				if err := bonder.ConnectInterface(iface); err != nil {
					log.Printf("[ipc] connect %s: %v", iface.FriendlyName, err)
					errors = append(errors, fmt.Sprintf("%s: %v", iface.FriendlyName, err))
				}
			}
		}
		if len(errors) > 0 {
			return errResp(req.ID, fmt.Sprintf("partial failure: %v", errors))
		}
		return okResp(req.ID)

	case MsgDisconnectAll:
		bonder.Stop()
		return okResp(req.ID)

	default:
		return errResp(req.ID, "unknown message type: "+string(req.Type))
	}
}

func okResp(id string) Response {
	payload, _ := json.Marshal(map[string]bool{"success": true})
	return Response{ID: id, Type: MsgOK, Payload: payload}
}

func errResp(id, msg string) Response {
	payload, _ := json.Marshal(ErrorPayload{Message: msg})
	return Response{ID: id, Type: MsgError, Payload: payload}
}
