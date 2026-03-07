lines = open('internal/wireguard/manager.go').readlines()
lines[99] = (
    'return fmt.Sprintf("[Interface]\\r\\nPrivateKey = %s\\r\\nAddress = %s/32\\r\\n'
    'DNS = 1.1.1.1\\r\\nTable = off\\r\\n%s\\r\\n[Peer]\\r\\nPublicKey = %s\\r\\n'
    'Endpoint = %s:%d\\r\\nAllowedIPs = 0.0.0.0/1, 128.0.0.0/1\\r\\n'
    'PersistentKeepalive = 25\\r\\n",\n'
)
open('internal/wireguard/manager.go', 'w').writelines(lines)
print('done')
print('line 100:', repr(lines[99][:60]))
