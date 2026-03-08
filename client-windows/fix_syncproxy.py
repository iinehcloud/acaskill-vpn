bonder_path = r'internal\bonding\bonder.go'
src = open(bonder_path).read()

old = 'log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)\nreturn nil\n}'
new = 'log.Printf("[bonding] OK %s connected vpn-ip=%s gw=%s", iface.FriendlyName, tc.AssignedIP, gatewayIP)\nb.syncProxy()\nreturn nil\n}'

if old in src:
    src = src.replace(old, new)
    open(bonder_path, 'w').write(src)
    print('Patch ConnectInterface syncProxy: OK')
else:
    print('ERROR: pattern not found')
    idx = src.find('OK %s connected vpn-ip=%s gw=%s')
    print(repr(src[idx-5:idx+120]))
