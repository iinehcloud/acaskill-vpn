package routing

import (
"fmt"
"log"
"net"
"os/exec"
"strings"
)

type TunnelRoute struct {
ServerIP    string
GatewayIP   string
InterfaceIP string
IfaceName   string
MetricBase  int
}

func AddHostRoute(r TunnelRoute) error {
if r.ServerIP == "" || r.GatewayIP == "" {
return fmt.Errorf("ServerIP and GatewayIP required")
}
args := []string{"add", r.ServerIP, "mask", "255.255.255.255", r.GatewayIP, "metric", fmt.Sprintf("%d", r.MetricBase)}
if idx := ifaceIndex(r.IfaceName); idx > 0 {
args = append(args, "if", fmt.Sprintf("%d", idx))
}
out, err := exec.Command("route", args...).CombinedOutput()
if err != nil {
outStr := string(out)
if strings.Contains(outStr, "already exists") || strings.Contains(outStr, "object already") {
log.Printf("[routing] route %s via %s already exists", r.ServerIP, r.GatewayIP)
return nil
}
return fmt.Errorf("route add: %w\n%s", err, outStr)
}
log.Printf("[routing] + %s/32 via %s (%s) metric=%d", r.ServerIP, r.GatewayIP, r.IfaceName, r.MetricBase)
return nil
}

func RemoveHostRoute(serverIP, gatewayIP string) {
if serverIP == "" || gatewayIP == "" { return }
out, err := exec.Command("route", "delete", serverIP, "mask", "255.255.255.255", gatewayIP).CombinedOutput()
if err != nil {
outStr := string(out)
if strings.Contains(outStr, "not found") || strings.Contains(outStr, "Element not found") { return }
log.Printf("[routing] route delete warning: %v", err)
return
}
log.Printf("[routing] - %s/32 via %s removed", serverIP, gatewayIP)
}

func CleanupServerRoutes(serverIP string) {
if serverIP == "" { return }
exec.Command("route", "delete", serverIP).CombinedOutput()
log.Printf("[routing] cleaned up stale routes for %s", serverIP)
}

func GetGatewayForInterface(ifaceIP string) string {
out, err := exec.Command("route", "print", "0.0.0.0").CombinedOutput()
if err != nil { return "" }
lines := strings.Split(string(out), "\n")
for _, line := range lines {
fields := strings.Fields(line)
if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" && fields[3] == ifaceIP {
gw := net.ParseIP(fields[2])
if gw != nil && !gw.IsUnspecified() && fields[2] != "On-link" {
return fields[2]
}
}
}
for _, line := range lines {
fields := strings.Fields(line)
if len(fields) >= 3 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" && fields[2] != "On-link" {
if gw := net.ParseIP(fields[2]); gw != nil && !gw.IsUnspecified() {
return fields[2]
}
}
}
return ""
}

func ResolveServerIP(hostname string) (string, error) {
if ip := net.ParseIP(hostname); ip != nil { return hostname, nil }
addrs, err := net.LookupHost(hostname)
if err != nil { return "", fmt.Errorf("resolve %s: %w", hostname, err) }
for _, addr := range addrs {
if ip := net.ParseIP(addr); ip != nil && ip.To4() != nil { return addr, nil }
}
return "", fmt.Errorf("no IPv4 for %s", hostname)
}

func ifaceIndex(name string) int {
ifaces, err := net.Interfaces()
if err != nil { return 0 }
for _, iface := range ifaces {
if iface.Name == name { return iface.Index }
}
return 0
}
