lines = open('internal/bonding/bonder.go').readlines()
# Remove leftover lines 286-291 (0-indexed: 286-290)
del lines[286:291]
open('internal/bonding/bonder.go', 'w').writelines(lines)
print('done, total lines:', len(lines))
# Verify
for i, l in enumerate(lines[274:295], start=275):
    print(f"{i}: {repr(l.rstrip())}")
