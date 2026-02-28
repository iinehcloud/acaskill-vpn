package config

import (
"encoding/json"
"fmt"
"os"
"path/filepath"
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
return cfg, nil
}

func (c *Config) Save() error {
os.MkdirAll(dataDir(), 0755)
data, err := json.MarshalIndent(c, "", "  ")
if err != nil { return err }
return os.WriteFile(filepath.Join(dataDir(), "config.json"), data, 0600)
}

func (c *Config) IsProvisioned() bool { return c.LicenseKey != "" && c.DeviceID != "" }
func (c *Config) KeysDir() string { return filepath.Join(c.DataDir, "keys") }
