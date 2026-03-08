import sys, os

# ── 1. Patch main.go to start the proxy ──────────────────────────────────────
main_path = r'cmd\daemon\main.go'
src = open(main_path).read()

old_import = '"github.com/acaskill/vpn-client/internal/ipc"'
new_import = '''"github.com/acaskill/vpn-client/internal/ipc"
\t"github.com/acaskill/vpn-client/internal/proxy"'''

if '"github.com/acaskill/vpn-client/internal/proxy"' not in src:
    src = src.replace(old_import, new_import)
    print('Patch main.go imports: OK')
else:
    print('Patch main.go imports: already done')

# Add proxy start in runDebug after bonder.Start
old_debug = '''if err := bonder.Start(ctx); err != nil {
\tlog.Fatalf("[daemon] bonder start failed: %v", err)
}
\tlog.Println("[daemon] bonding engine started")'''

new_debug = '''if err := bonder.Start(ctx); err != nil {
\tlog.Fatalf("[daemon] bonder start failed: %v", err)
}
\tlog.Println("[daemon] bonding engine started")

\t// Start bonding proxy
\tbondProxy := proxy.New("127.0.0.1:1080")
\tif err := bondProxy.Start(ctx); err != nil {
\t\tlog.Printf("[daemon] proxy start failed: %v", err)
\t} else {
\t\tbonder.SetProxy(bondProxy)
\t\tlog.Println("[daemon] bonding proxy started on 127.0.0.1:1080")
\t}'''

if 'bondProxy' not in src:
    if old_debug in src:
        src = src.replace(old_debug, new_debug)
        print('Patch main.go runDebug: OK')
    else:
        print('ERROR: could not find bonder.Start block in main.go')
        for i, l in enumerate(src.splitlines()):
            if 'bonder.Start' in l:
                print(f'  line {i+1}: {repr(l)}')
else:
    print('Patch main.go runDebug: already done')

# Also patch the Execute method (service mode)
old_exec = '''s.bonder.Start(ctx)
\ts.server.Start(ctx, s.bonder)'''
new_exec = '''s.bonder.Start(ctx)
\tbondProxy := proxy.New("127.0.0.1:1080")
\tif err := bondProxy.Start(ctx); err == nil {
\t\ts.bonder.SetProxy(bondProxy)
\t}
\ts.server.Start(ctx, s.bonder)'''

if 'bondProxy' not in src or old_exec in src:
    if old_exec in src:
        src = src.replace(old_exec, new_exec)
        print('Patch main.go Execute: OK')
    else:
        print('ERROR: could not find Execute block')
else:
    print('Patch main.go Execute: already done')

open(main_path, 'w').write(src)

# ── 2. Patch bonder.go to hold proxy ref and call UpdateTunnels ──────────────
bonder_path = r'internal\bonding\bonder.go'
src = open(bonder_path).read()

# Add proxy import
old_imp = '"github.com/acaskill/vpn-client/internal/wireguard"'
new_imp = '''"github.com/acaskill/vpn-client/internal/proxy"
\t"github.com/acaskill/vpn-client/internal/wireguard"'''
if '"github.com/acaskill/vpn-client/internal/proxy"' not in src:
    src = src.replace(old_imp, new_imp)
    print('Patch bonder.go imports: OK')
else:
    print('Patch bonder.go imports: already done')

# Add proxy field to Bonder struct
old_struct = '''type Bonder struct {
cfg      *config.Config
wgMgr    *wireguard.Manager
tunnels  map[string]*TunnelState
serverIP string
mu       sync.RWMutex
running  bool
}'''
new_struct = '''type Bonder struct {
cfg      *config.Config
wgMgr    *wireguard.Manager
tunnels  map[string]*TunnelState
serverIP string
bondProxy *proxy.Proxy
mu       sync.RWMutex
running  bool
}'''
if 'bondProxy' not in src:
    if old_struct in src:
        src = src.replace(old_struct, new_struct)
        print('Patch bonder.go struct: OK')
    else:
        print('ERROR: Bonder struct not found')
        for i, l in enumerate(src.splitlines()):
            if 'type Bonder struct' in l:
                print(f'  line {i+1}: {repr(l)}')
else:
    print('Patch bonder.go struct: already done')

# Add SetProxy method after New()
old_new = '''func New(cfg *config.Config) *Bonder {
return &Bonder{cfg: cfg, wgMgr: wireguard.New(cfg), tunnels: make(map[string]*TunnelState)}
}'''
new_new = '''func New(cfg *config.Config) *Bonder {
return &Bonder{cfg: cfg, wgMgr: wireguard.New(cfg), tunnels: make(map[string]*TunnelState)}
}

func (b *Bonder) SetProxy(p *proxy.Proxy) {
b.mu.Lock()
b.bondProxy = p
b.mu.Unlock()
b.syncProxy()
}

func (b *Bonder) syncProxy() {
b.mu.RLock()
p := b.bondProxy
var ifaces []proxy.TunnelIface
for _, t := range b.tunnels {
t.mu.Lock()
if t.IsConnected {
ifaces = append(ifaces, proxy.TunnelIface{Name: "acaskill-" + sanitize(t.Interface.Name), AssignedIP: t.AssignedIP})
}
t.mu.Unlock()
}
b.mu.RUnlock()
if p != nil {
p.UpdateTunnels(ifaces)
}
}'''

if 'SetProxy' not in src:
    if old_new in src:
        src = src.replace(old_new, new_new)
        print('Patch bonder.go SetProxy: OK')
    else:
        print('ERROR: New() not found')
else:
    print('Patch bonder.go SetProxy: already done')

# Call syncProxy after ConnectInterface succeeds (before return nil at end of ConnectInterface)
old_connect_end = '''log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)
return nil
}'''
new_connect_end = '''log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)
b.syncProxy()
return nil
}'''
if 'b.syncProxy()' not in src:
    if old_connect_end in src:
        src = src.replace(old_connect_end, new_connect_end, 1)
        print('Patch bonder.go ConnectInterface syncProxy: OK')
    else:
        print('ERROR: ConnectInterface end not found')
        for i, l in enumerate(src.splitlines()):
            if 'OK %s connected' in l:
                print(f'  line {i+1}: {repr(l)}')
else:
    print('Patch bonder.go ConnectInterface syncProxy: already done')

# Call syncProxy after DisconnectInterface
old_disc = '''b.teardownTunnel(tunnel)
delete(b.tunnels, ifaceName)
log.Printf("[bonding] %s disconnected", ifaceName)
return nil
}'''
new_disc = '''b.teardownTunnel(tunnel)
delete(b.tunnels, ifaceName)
log.Printf("[bonding] %s disconnected", ifaceName)
b.syncProxy()
return nil
}'''
if old_disc in src:
    src = src.replace(old_disc, new_disc)
    print('Patch bonder.go DisconnectInterface syncProxy: OK')
else:
    print('Patch bonder.go DisconnectInterface syncProxy: already done or not found')

open(bonder_path, 'w').write(src)
print('\nAll patches done.')
