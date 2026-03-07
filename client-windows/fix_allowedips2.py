# The correct way to do full tunnel on WireGuard without routing loop:
# Instead of AllowedIPs = 0.0.0.0/0 (which catches everything including WG UDP),
# We use the two-route trick that covers all IPs EXCEPT the server IP.
# The host route we add before tunnel startup handles the server IP directly.
#
# AllowedIPs = 0.0.0.0/1, 128.0.0.0/1
# This covers ALL IPv4 space (split into two /1 routes) which Windows treats
# as less specific than the /32 host route we added for the server IP.
# Result: server UDP goes via physical interface (host route wins), 
#         everything else goes through the tunnel.

src = open('internal/wireguard/manager.go').read()

for old, new in [
    ('AllowedIPs = 0.0.0.0/0', 'AllowedIPs = 0.0.0.0/1, 128.0.0.0/1'),
    ('AllowedIPs = 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16', 'AllowedIPs = 0.0.0.0/1, 128.0.0.0/1'),
]:
    if old in src:
        src = src.replace(old, new)
        print(f'Patched: {old} -> {new}')
        break
else:
    print('ERROR: AllowedIPs not found. Lines containing AllowedIPs:')
    for i, line in enumerate(src.splitlines()):
        if 'Allowed' in line:
            print(f'  {i+1}: {repr(line)}')

open('internal/wireguard/manager.go', 'w').write(src)
