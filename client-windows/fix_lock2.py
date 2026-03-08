path = r'internal\bonding\bonder.go'
src = open(path).read()

# Step 1: Replace the lock+check at the top
old1 = 'func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {\nb.mu.Lock(); defer b.mu.Unlock()\nif _, exists := b.tunnels[iface.Name]; exists {\nlog.Printf("[bonding] %s already connected", iface.FriendlyName)\nreturn nil\n}'
new1 = '''func (b *Bonder) ConnectInterface(iface interfaces.NetworkInterface) error {
b.mu.Lock()
if _, exists := b.tunnels[iface.Name]; exists {
b.mu.Unlock()
log.Printf("[bonding] %s already connected", iface.FriendlyName)
return nil
}
serverIP := b.serverIP
tunnelCount := len(b.tunnels)
b.mu.Unlock()'''

if old1 in src:
    src = src.replace(old1, new1)
    print('Patch 1 (unlock early): OK')
else:
    print('ERROR patch 1')

# Step 2: Replace the serverIP read (was using b.serverIP under lock, now use local var)
old2 = 'serverIP := b.serverIP\nif serverIP == "" { serverIP, _ = routing.ResolveServerIP(b.cfg.VPNHost) }\nif serverIP != "" {\nmetric := 1 + len(b.tunnels)'
new2 = 'if serverIP == "" { serverIP, _ = routing.ResolveServerIP(b.cfg.VPNHost) }\nif serverIP != "" {\nmetric := 1 + tunnelCount'

if old2 in src:
    src = src.replace(old2, new2)
    print('Patch 2 (use local serverIP/tunnelCount): OK')
else:
    print('ERROR patch 2')
    idx = src.find('serverIP := b.serverIP')
    if idx >= 0:
        print(repr(src[idx:idx+150]))

# Step 3: Replace final write - add explicit lock around tunnel map write
old3 = 'b.tunnels[iface.Name] = &TunnelState{'
new3 = 'b.mu.Lock()\nb.tunnels[iface.Name] = &TunnelState{'

# Only patch the one inside ConnectInterface (not if there are others)
count = src.count(old3)
print(f'Found {count} occurrence(s) of tunnel write')
if count == 1:
    src = src.replace(old3, new3)
    print('Patch 3 (re-lock for write): OK')

# Step 4: Unlock after the Weight field
old4 = 'Weight:      1.0,\n}\nlog.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)\nb.syncProxy()'
new4 = 'Weight:      1.0,\n}\nb.mu.Unlock()\nlog.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)\nb.syncProxy()'

if old4 in src:
    src = src.replace(old4, new4)
    print('Patch 4 (unlock after write): OK')
else:
    print('ERROR patch 4')
    idx = src.find('Weight:      1.0,')
    if idx >= 0:
        print(repr(src[idx:idx+200]))

open(path, 'w').write(src)
print('\nDone.')
