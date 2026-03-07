src = open('internal/bonding/bonder.go').read()

# Find the sendHeartbeat call and add updateBandwidth after it
# Try different indentation patterns
patches = [
    ('b.sendHeartbeat(t.AssignedIP, latency)\n\t\t\t\tb.updateBandwidth(t)', None),  # already patched?
    ('b.sendHeartbeat(t.AssignedIP, latency)', 'b.sendHeartbeat(t.AssignedIP, latency)\n\t\t\t\tb.updateBandwidth(t)'),
]

patched = False
for old, new in patches:
    if old in src and new and old != new:
        src = src.replace(old, new, 1)
        patched = True
        print(f'Patched sendHeartbeat call')
        break

if not patched:
    print('ERROR: Could not find sendHeartbeat call. Searching...')
    for i, line in enumerate(src.splitlines()):
        if 'sendHeartbeat' in line or 'updateBandwidth' in line:
            print(f'  line {i+1}: {repr(line)}')

open('internal/bonding/bonder.go', 'w').write(src)

# Verify
for i, line in enumerate(src.splitlines()):
    if 'sendHeartbeat' in line or 'updateBandwidth' in line:
        print(f'  line {i+1}: {line}')
