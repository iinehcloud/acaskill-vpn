main_path = r'cmd\daemon\main.go'
src = open(main_path).read()

# ── Patch 1: Execute method (service mode) ───────────────────
old1 = 's.bonder.Start(ctx)\ns.server.Start(ctx, s.bonder)'
new1 = '''s.bonder.Start(ctx)
bondProxy := proxy.New("127.0.0.1:1080")
if err := bondProxy.Start(ctx); err == nil {
s.bonder.SetProxy(bondProxy)
}
s.server.Start(ctx, s.bonder)'''

if 'bondProxy' not in src:
    if old1 in src:
        src = src.replace(old1, new1)
        print('Patch Execute: OK')
    else:
        print('ERROR: Execute block not found')
else:
    print('Patch Execute: already done')

# ── Patch 2: runDebug function ────────────────────────────────
old2 = 'if err := bonder.Start(ctx); err != nil {\nlog.Fatalf("[daemon] bonder start failed: %v", err)\n}\nlog.Println("[daemon] bonding engine started")'
new2 = '''if err := bonder.Start(ctx); err != nil {
log.Fatalf("[daemon] bonder start failed: %v", err)
}
log.Println("[daemon] bonding engine started")

// Start bonding proxy
bondProxy := proxy.New("127.0.0.1:1080")
if err := bondProxy.Start(ctx); err != nil {
log.Printf("[daemon] proxy start failed: %v", err)
} else {
bonder.SetProxy(bondProxy)
log.Println("[daemon] bonding proxy started on 127.0.0.1:1080")
}'''

if old2 in src:
    src = src.replace(old2, new2)
    print('Patch runDebug: OK')
else:
    print('ERROR: runDebug block not found, trying alternate...')
    # Try with \r\n
    old2b = old2.replace('\n', '\r\n')
    if old2b in src:
        src = src.replace(old2b, new2)
        print('Patch runDebug (CRLF): OK')
    else:
        print('FAILED. Dumping context:')
        idx = src.find('bonder.Start(ctx)')
        print(repr(src[max(0,idx-20):idx+300]))

open(main_path, 'w').write(src)
print('\nDone.')
