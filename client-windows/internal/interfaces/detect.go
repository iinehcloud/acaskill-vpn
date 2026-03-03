package interfaces

import (
"log"
"net"
"strings"
)

type InterfaceType string

const (
TypeWiFi     InterfaceType = "WiFi"
TypeEthernet InterfaceType = "Ethernet"
TypeMobile   InterfaceType = "Mobile"
TypeHotspot  InterfaceType = "Hotspot"
TypeOther    InterfaceType = "Other"
)

type NetworkInterface struct {
Name         string        `json:"name"`
FriendlyName string        `json:"friendlyName"`
Type         InterfaceType `json:"type"`
IP           net.IP        `json:"ip"`
Gateway      net.IP        `json:"gateway,omitempty"`
IsConnected  bool          `json:"isConnected"`
}

func (n NetworkInterface) Label() string {
if n.FriendlyName != "" {
return n.FriendlyName
}
return n.Name
}

func Detect() ([]NetworkInterface, error) {
ifaces, err := net.Interfaces()
if err != nil {
return nil, err
}

var result []NetworkInterface

for _, iface := range ifaces {
if iface.Flags&net.FlagUp == 0 { continue }
if iface.Flags&net.FlagLoopback != 0 { continue }
if shouldSkip(iface.Name) { continue }

ip := getIPv4(iface)
if ip == nil { continue }
if ip.IsLinkLocalUnicast() { continue }

ni := NetworkInterface{
Name:         iface.Name,
FriendlyName: iface.Name,
Type:         classifyInterface(iface.Name),
IP:           ip,
IsConnected:  true,
}

log.Printf("[detect] %s (%s) IP=%s type=%s", iface.Name, iface.HardwareAddr, ip, ni.Type)
result = append(result, ni)
}

return result, nil
}

func getIPv4(iface net.Interface) net.IP {
addrs, err := iface.Addrs()
if err != nil { return nil }
for _, addr := range addrs {
var ip net.IP
switch v := addr.(type) {
case *net.IPNet:  ip = v.IP
case *net.IPAddr: ip = v.IP
}
if ip == nil { continue }
if v4 := ip.To4(); v4 != nil && !v4.IsLoopback() && !v4.IsLinkLocalUnicast() {
return v4
}
}
return nil
}

func shouldSkip(name string) bool {
lower := strings.ToLower(name)
for _, skip := range []string{"loopback","vethernet","vmware","virtualbox","docker","hyper","acaskill","tailscale","zerotier","wg"} {
if strings.Contains(lower, skip) { return true }
}
return false
}

func classifyInterface(name string) InterfaceType {
lower := strings.ToLower(name)
switch {
case strings.Contains(lower, "wi-fi") || strings.Contains(lower, "wifi") || strings.Contains(lower, "wlan"):
return TypeWiFi
case strings.Contains(lower, "ethernet") && (strings.Contains(lower, "usb") || strings.Contains(lower, "ncm") || strings.Contains(lower, "rndis")):
return TypeMobile
case strings.Contains(lower, "ethernet"):
return TypeEthernet
case strings.Contains(lower, "mobile") || strings.Contains(lower, "lte") || strings.Contains(lower, "4g"):
return TypeMobile
default:
return TypeOther
}
}
