package wireguard

import (
"encoding/json"
"fmt"
"net/http"
"os"
"path/filepath"
"strings"
"time"

"github.com/acaskill/vpn-client/internal/config"
"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type KeyPair struct {
PrivateKey string
PublicKey  string
}

type TunnelConfig struct {
InterfaceName string
InterfaceIP   string
PrivateKey    string
PublicKey     string
AssignedIP    string
ServerPubKey  string
ServerHost    string
ServerPort    int
Label         string
}

type Manager struct {
cfg     *config.Config
keysDir string
}

func New(cfg *config.Config) *Manager {
return &Manager{cfg: cfg, keysDir: cfg.KeysDir()}
}

func (m *Manager) GenerateKeyPair(label string) (*KeyPair, error) {
os.MkdirAll(m.keysDir, 0700)
keyFile := filepath.Join(m.keysDir, sanitizeLabel(label)+".key")
if data, err := os.ReadFile(keyFile); err == nil {
var kp KeyPair
if err := json.Unmarshal(data, &kp); err == nil { return &kp, nil }
}
privKey, err := wgtypes.GeneratePrivateKey()
if err != nil { return nil, fmt.Errorf("generate key: %w", err) }
kp := &KeyPair{PrivateKey: privKey.String(), PublicKey: privKey.PublicKey().String()}
data, _ := json.Marshal(kp)
os.WriteFile(keyFile, data, 0600)
return kp, nil
}

func (m *Manager) ProvisionPeer(deviceID, publicKey, label string) (*TunnelConfig, error) {
type req struct {
DeviceID       string `json:"deviceId"`
PublicKey      string `json:"publicKey"`
LicenseKey     string `json:"licenseKey"`
InterfaceLabel string `json:"interfaceLabel"`
}
type resp struct {
OK           bool   `json:"ok"`
AssignedIP   string `json:"assignedIp"`
ServerPubKey string `json:"serverPubKey"`
ServerPort   int    `json:"serverPort"`
Error        string `json:"error"`
}
body, _ := json.Marshal(req{DeviceID: deviceID, PublicKey: publicKey, LicenseKey: m.cfg.LicenseKey, InterfaceLabel: label})
client := &http.Client{Timeout: 15 * time.Second}
r, err := client.Post(m.cfg.APIBase+"/provision/peer", "application/json", strings.NewReader(string(body)))
if err != nil { return nil, fmt.Errorf("provision: %w", err) }
defer r.Body.Close()
var result resp
json.NewDecoder(r.Body).Decode(&result)
if !result.OK { return nil, fmt.Errorf("server: %s", result.Error) }
return &TunnelConfig{AssignedIP: result.AssignedIP, ServerPubKey: result.ServerPubKey, ServerHost: m.cfg.VPNHost, ServerPort: result.ServerPort, Label: label}, nil
}

func BuildWgConfig(tc *TunnelConfig) string {
return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress    = %s/32\nDNS        = 1.1.1.1\n\n[Peer]\nPublicKey  = %s\nEndpoint   = %s:%d\nAllowedIPs = 0.0.0.0/0\nPersistentKeepalive = 25\n",
tc.PrivateKey, tc.AssignedIP, tc.ServerPubKey, tc.ServerHost, tc.ServerPort)
}

func sanitizeLabel(label string) string {
label = strings.ToLower(label)
label = strings.ReplaceAll(label, " ", "-")
label = strings.ReplaceAll(label, "/", "-")
return label
}
