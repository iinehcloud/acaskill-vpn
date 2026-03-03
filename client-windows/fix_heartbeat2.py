import re

src = open('internal/bonding/bonder.go').read()

# Print lines 270-295 so we can see what's there
lines = src.splitlines()
for i, line in enumerate(lines[270:295], start=271):
    print(f"{i}: {repr(line)}")
