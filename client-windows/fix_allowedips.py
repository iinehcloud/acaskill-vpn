src = open('internal/wireguard/manager.go').read()

# Find and replace the AllowedIPs line in BuildWgConfig
old = 'AllowedIPs = 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16'
new = 'AllowedIPs = 0.0.0.0/0'

if old in src:
    src = src.replace(old, new)
    print('Patched AllowedIPs to 0.0.0.0/0')
else:
    print('ERROR: Could not find AllowedIPs line. Searching...')
    for i, line in enumerate(src.splitlines()):
        if 'AllowedIPs' in line or 'allowedips' in line.lower():
            print(f'  line {i+1}: {repr(line)}')

open('internal/wireguard/manager.go', 'w').write(src)
