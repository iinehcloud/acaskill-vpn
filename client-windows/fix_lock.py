bonder_path = r'internal\bonding\bonder.go'
src = open(bonder_path).read()

old = '''func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
b.mu.Lock(); defer b.mu.Unlock()
if _, exists := b.tunnels[iface.Name]; exists {
log.Printf("[bonding] %s already connected", iface.FriendlyName)
return nil
}
log.Printf("[bonding] connecting: %s (IP: %s)", iface.FriendlyName, iface.IP)
gatewayIP := routing.GetGatewayForInterface(iface.IP.String())
if gatewayIP == "" && iface.Gateway != nil {
gatewayIP = iface.Gateway.String()
}
if gatewayIP == "" {
return fmt.Errorf("no gateway for %s - is it connected to internet?", iface.FriendlyName)
}
log.Printf("[bonding] %s gateway: %s", iface.FriendlyName, gatewayIP)
kp, err := b.wgMgr.GenerateKeyPair(iface.Label())
if err != nil { return fmt.Errorf("generate keys for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s pubkey: %s", iface.FriendlyName, kp.PublicKey)
tc, err := b.wgMgr.ProvisionPeer(b.cfg.DeviceID, kp.PublicKey, iface.Label())
if err != nil { return fmt.Errorf("provision peer for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s provisioned -> IP=%s", iface.FriendlyName, tc.AssignedIP)
tc.PrivateKey    = kp.PrivateKey
tc.PublicKey     = kp.PublicKey
tc.InterfaceName = iface.Name
tc.InterfaceIP   = iface.IP.String()
tc.GatewayIP     = gatewayIP
serverIP := b.serverIP
if serverIP == "" { serverIP, _ = routing.ResolveServerIP(b.cfg.VPNHost) }
if serverIP != "" {
metric := 1 + len(b.tunnels)
if routeErr := routing.AddHostRoute(routing.TunnelRoute{
ServerIP:    serverIP,
GatewayIP:   gatewayIP,
InterfaceIP: iface.IP.String(),
IfaceName:   iface.Name,
MetricBase:  metric,
}); routeErr != nil {
log.Printf("[bonding] warning: host route for %s: %v", iface.FriendlyName, routeErr)
}
}
if err := b.bringUpTunnel(tc); err != nil {
if serverIP != "" { routing.RemoveHostRoute(serverIP, gatewayIP) }
return fmt.Errorf("bring up tunnel for %s: %w", iface.Name, err)
}
b.tunnels[iface.Name] = &TunnelState{
Interface:   iface,
TunnelCfg:   tc,
KeyPair:     kp,
AssignedIP:  tc.AssignedIP,
GatewayIP:   gatewayIP,
ServerIP:    serverIP,
IsConnected: true,
LastSeen:    time.Now(),
Weight:      1.0,
}
log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)
b.syncProxy()
return nil
}'''

new = '''func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
// Check already connected (short lock)
b.mu.Lock()
if _, exists := b.tunnels[iface.Name]; exists {
b.mu.Unlock()
log.Printf("[bonding] %s already connected", iface.FriendlyName)
return nil
}
serverIP := b.serverIP
tunnelCount := len(b.tunnels)
b.mu.Unlock()

// All slow work (HTTP provisioning, WireGuard setup) done WITHOUT the lock
log.Printf("[bonding] connecting: %s (IP: %s)", iface.FriendlyName, iface.IP)
gatewayIP := routing.GetGatewayForInterface(iface.IP.String())
if gatewayIP == "" && iface.Gateway != nil {
gatewayIP = iface.Gateway.String()
}
if gatewayIP == "" {
return fmt.Errorf("no gateway for %s - is it connected to internet?", iface.FriendlyName)
}
log.Printf("[bonding] %s gateway: %s", iface.FriendlyName, gatewayIP)
kp, err := b.wgMgr.GenerateKeyPair(iface.Label())
if err != nil { return fmt.Errorf("generate keys for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s pubkey: %s", iface.FriendlyName, kp.PublicKey)
tc, err := b.wgMgr.ProvisionPeer(b.cfg.DeviceID, kp.PublicKey, iface.Label())
if err != nil { return fmt.Errorf("provision peer for %s: %w", iface.Name, err) }
log.Printf("[bonding] %s provisioned -> IP=%s", iface.FriendlyName, tc.AssignedIP)
tc.PrivateKey    = kp.PrivateKey
tc.PublicKey     = kp.PublicKey
tc.InterfaceName = iface.Name
tc.InterfaceIP   = iface.IP.String()
tc.GatewayIP     = gatewayIP
if serverIP == "" { serverIP, _ = routing.ResolveServerIP(b.cfg.VPNHost) }
if serverIP != "" {
metric := 1 + tunnelCount
if routeErr := routing.AddHostRoute(routing.TunnelRoute{
ServerIP:    serverIP,
GatewayIP:   gatewayIP,
InterfaceIP: iface.IP.String(),
IfaceName:   iface.Name,
MetricBase:  metric,
}); routeErr != nil {
log.Printf("[bonding] warning: host route for %s: %v", iface.FriendlyName, routeErr)
}
}
if err := b.bringUpTunnel(tc); err != nil {
if serverIP != "" { routing.RemoveHostRoute(serverIP, gatewayIP) }
return fmt.Errorf("bring up tunnel for %s: %w", iface.Name, err)
}

// Re-lock only to write result
b.mu.Lock()
b.tunnels[iface.Name] = &TunnelState{
Interface:   iface,
TunnelCfg:   tc,
KeyPair:     kp,
AssignedIP:  tc.AssignedIP,
GatewayIP:   gatewayIP,
ServerIP:    serverIP,
IsConnected: true,
LastSeen:    time.Now(),
Weight:      1.0,
}
b.mu.Unlock()
log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)
b.syncProxy()
return nil
}'''

if old in src:
    src = src.replace(old, new)
    open(bonder_path, 'w').write(src)
    print('Fix ConnectInterface lock: OK')
else:
    print('ERROR: pattern not found')
    idx = src.find('func (b *Bonder) ConnectInterface')
    print(repr(src[idx:idx+200]))
