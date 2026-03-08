import sys

main_path = r'cmd\daemon\main.go'
src = open(main_path).read()

print("=== Scanning main.go for relevant lines ===")
for i, l in enumerate(src.splitlines()):
    if 'bonder.Start' in l or 'server.Start' in l or 'bondProxy' in l:
        print(f"  line {i+1}: {repr(l)}")

print("\n=== Raw bytes around bonder.Start (service Execute) ===")
idx = src.find('s.bonder.Start(ctx)')
if idx >= 0:
    print(repr(src[idx-5:idx+80]))

print("\n=== Raw bytes around bonder.Start (runDebug) ===")
idx2 = src.find('if err := bonder.Start(ctx)')
if idx2 >= 0:
    print(repr(src[idx2-5:idx2+200]))
