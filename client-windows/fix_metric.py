src = open('internal/bonding/bonder.go').read()

# Change metric calculation to use 1 and 2 instead of 10 and 20
old = 'metric := 10 + len(b.tunnels)*10'
new = 'metric := 1 + len(b.tunnels)'

if old in src:
    src = src.replace(old, new)
    print('patched metric to 1+n')
else:
    print('searching for metric line...')
    for i, l in enumerate(src.splitlines()):
        if 'metric' in l.lower() and 'MetricBase' not in l and 'TunnelRoute' not in l:
            print(f'  {i+1}: {repr(l)}')

open('internal/bonding/bonder.go', 'w').write(src)
