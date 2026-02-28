package interfaces

import (
"fmt"
"net"
"strings"
)

type InterfaceType string
const (
TypeWiFi     InterfaceType = "WiFi"
TypeEthernet InterfaceType = "Ethernet"
TypeMobile   InterfaceType = "Mobile"
TypeOther    InterfaceType = "Other"
)

type NetworkInterface struct {
Name         string
FriendlyName string
Type         InterfaceType
IP           net.IP
Gateway      net.IP
MACAddr      string
IsConnected  bool
}

func Detect() ([]NetworkInterface, error) {
ifaces, err := net.Interfaces()
if err != nil { return nil, fmt.Errorf("list interfaces: %w", err) }
var result []NetworkInterface
for _, iface := range ifaces {
if iface.Flags&net.FlagLoopback != 0 { continue }
if iface.Flags&net.FlagUp == 0 { continue }
if isVirtual(iface.Name) { continue }
addrs, err := iface.Addrs()
if err != nil || len(addrs) == 0 { continue }
var ip net.IP
for _, addr := range addrs {
if ipNet, ok := addr.(*net.IPNet); ok {
if v4 := ipNet.IP.To4(); v4 != nil { ip = v4; break }
}
}
if ip == nil { continue }
if ip[0] == 169 && ip[1] == 254 { continue }
result = append(result, NetworkInterface{
Name:         iface.Name,
FriendlyName: iface.Name,
Type:         classifyInterface(iface.Name),
IP:           ip,
MACAddr:      iface.HardwareAddr.String(),
IsConnected:  true,
})
}
return result, nil
}

func isVirtual(name string) bool {
skip := []string{"loopback","isatap","6to4","teredo","vethernet","acaskill","wg","tun","tap","vmnet","virtualbox"}
n := strings.ToLower(name)
for _, s := range skip {
if strings.Contains(n, s) { return true }
}
return false
}

func classifyInterface(name string) InterfaceType {
n := strings.ToLower(name)
if strings.Contains(n, "wi-fi") || strings.Contains(n, "wifi") || strings.Contains(n, "wireless") { return TypeWiFi }
if strings.Contains(n, "ethernet") || strings.Contains(n, "local area") { return TypeEthernet }
if strings.Contains(n, "mobile") || strings.Contains(n, "modem") || strings.Contains(n, "rndis") { return TypeMobile }
return TypeOther
}

func (ni *NetworkInterface) Label() string {
if ni.FriendlyName != "" { return ni.FriendlyName }
return string(ni.Type)
}
