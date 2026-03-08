src = open('internal/bonding/bonder.go').read()
old = '\tif rx > 0 || tx > 0 {\n\t\tt.mu.Lock()\n\t\tt.BytesRecv = rx\n\t\tt.BytesSent = tx\n\t\tt.mu.Unlock()\n\t}\n}'
new = '\tnow := time.Now()\n\tt.mu.Lock()\n\tif !t.LastBwTime.IsZero() {\n\t\telapsed := now.Sub(t.LastBwTime).Seconds()\n\t\tif elapsed > 0 {\n\t\t\tif tx >= t.PrevSent {\n\t\t\t\tt.SpeedTx = float64(tx-t.PrevSent) * 8 / elapsed / 1_000_000\n\t\t\t}\n\t\t\tif rx >= t.PrevRecv {\n\t\t\t\tt.SpeedRx = float64(rx-t.PrevRecv) * 8 / elapsed / 1_000_000\n\t\t\t}\n\t\t}\n\t}\n\tt.BytesSent = tx\n\tt.BytesRecv = rx\n\tt.PrevSent = tx\n\tt.PrevRecv = rx\n\tt.LastBwTime = now\n\tt.mu.Unlock()\n}'
if old in src:
    src = src.replace(old, new)
    print('Patch 6 OK')
else:
    print('ERROR - dumping lines around rx assignment:')
    for i, l in enumerate(src.splitlines()):
        if 'BytesRecv = rx' in l or 'BytesSent = tx' in l or 'rx > 0' in l:
            print(f'  {i+1}: {repr(l)}')
open('internal/bonding/bonder.go', 'w').write(src)
