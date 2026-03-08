bonder_path = r'internal\bonding\bonder.go'
src = open(bonder_path).read()

print("=== Lines around 'OK %s connected' ===")
for i, l in enumerate(src.splitlines()):
    if 'OK %s connected' in l or 'syncProxy' in l:
        print(f"  line {i+1}: {repr(l)}")

# Find the exact string and show context
idx = src.find('OK %s connected vpn-ip=%s gw=%s')
if idx >= 0:
    print("\n=== Raw bytes around it ===")
    print(repr(src[idx-5:idx+120]))
