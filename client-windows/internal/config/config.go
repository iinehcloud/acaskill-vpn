package config

import (
"bytes"
"crypto/rand"
"encoding/json"
"fmt"
"net/http"
"os"
"path/filepath"
"time"
)

const DefaultIPCPipe = `\\.\pipe\acaskill-vpn`

type Config struct {
APIBase       string `json:"apiBase"`
VPNHost       string `json:"vpnHost"`
VPNPort       int    `json:"vpnPort"`
ServerPubKey  string `json:"serverPubKey"`
LicenseKey    string `json:"licenseKey"`
DeviceID      string `json:"deviceId"`
DeviceName    string `json:"deviceName"`
HeartbeatMs   int    `json:"heartbeatMs"`
FailoverMs    int    `json:"failoverMs"`
AutoReconnect bool   `json:"autoReconnect"`
IPCPipe       string `json:"ipcPipe"`
DataDir       string `json:"-"`
}

func defaultConfig() *Config {
return &Config{
APIBase:       "https://api.acaskill.com",
VPNHost:       "vpn.acaskill.com",
VPNPort:       51820,
HeartbeatMs:   500,
FailoverMs:    3000,
AutoReconnect: true,
IPCPipe:       DefaultIPCPipe,
}
}

func dataDir() string {
base := os.Getenv("PROGRAMDATA")
if base == "" { base = os.Getenv("APPDATA") }
return filepath.Join(base, "AcaSkillVPN")
}

func Load() (*Config, error) {
cfg := defaultConfig()
cfg.DataDir = dataDir()
path := filepath.Join(dataDir(), "config.json")
data, err := os.ReadFile(path)
if os.IsNotExist(err) {
os.MkdirAll(cfg.DataDir, 0755)
cfg.Save()
return cfg, nil
}
if err != nil { return nil, fmt.Errorf("read config: %w", err) }
if err := json.Unmarshal(data, cfg); err != nil { return nil, fmt.Errorf("parse config: %w", err) }
cfg.DataDir = dataDir()
// Generate deviceId if missing
if cfg.DeviceID == "" {
cfg.DeviceID = newUUID()
cfg.Save()
}
return cfg, nil
}

func (c *Config) Save() error {
os.MkdirAll(dataDir(), 0755)
data, err := json.MarshalIndent(c, "", "  ")
if err != nil { return err }
return os.WriteFile(filepath.Join(dataDir(), "config.json"), data, 0600)
}

func (c *Config) IsProvisioned() bool { return c.LicenseKey != "" && c.DeviceID != "" }
func (c *Config) KeysDir() string     { return filepath.Join(c.DataDir, "keys") }

func (c *Config) ValidateAndRegister(licenseKey, deviceName string) error {
client := &http.Client{Timeout: 15 * time.Second}

type validateReq struct {
LicenseKey string `json:"licenseKey"`
}
type validateResp struct {
OK    bool   `json:"ok"`
Error string `json:"error"`
}

vBody, _ := json.Marshal(validateReq{LicenseKey: licenseKey})
resp, err := client.Post(c.APIBase+"/license/validate", "application/json", bytes.NewReader(vBody))
if err != nil { return fmt.Errorf("cannot reach server: %w", err) }
defer resp.Body.Close()

var vResult validateResp
json.NewDecoder(resp.Body).Decode(&vResult)
if !vResult.OK { return fmt.Errorf("%s", vResult.Error) }

type registerReq struct {
LicenseKey string `json:"licenseKey"`
DeviceName string `json:"deviceName"`
DeviceID   string `json:"deviceId"`
}
type registerResp struct {
OK       bool   `json:"ok"`
DeviceID string `json:"deviceId"`
Error    string `json:"error"`
}

// Ensure we have a UUID
if c.DeviceID == "" {
c.DeviceID = newUUID()
}

rBody, _ := json.Marshal(registerReq{LicenseKey: licenseKey, DeviceName: deviceName, DeviceID: c.DeviceID})
resp2, err := client.Post(c.APIBase+"/license/register", "application/json", bytes.NewReader(rBody))
if err != nil { return fmt.Errorf("register failed: %w", err) }
defer resp2.Body.Close()

var rResult registerResp
json.NewDecoder(resp2.Body).Decode(&rResult)
if !rResult.OK { return fmt.Errorf("%s", rResult.Error) }

c.LicenseKey = licenseKey
c.DeviceName = deviceName
if rResult.DeviceID != "" { c.DeviceID = rResult.DeviceID }
return c.Save()
}

// newUUID generates a random UUID v4
func newUUID() string {
b := make([]byte, 16)
rand.Read(b)
b[6] = (b[6] & 0x0f) | 0x40
b[8] = (b[8] & 0x3f) | 0x80
return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
