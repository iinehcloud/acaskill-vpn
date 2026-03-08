src = open('internal/bonding/bonder.go').read()

patches = [
    # Patch 1: TunnelState struct - already done
    # Patch 2: TunnelSnapshot struct - already done  
    # Patch 3: Status struct - already done

    # Patch 4: Snapshot() return - no leading tabs on these lines
    (
        'BytesSent:     t.BytesSent,\nBytesRecv:     t.BytesRecv,\nWeight:        t.Weight,',
        'BytesSent:     t.BytesSent,\nBytesRecv:     t.BytesRecv,\nSpeedTx:       t.SpeedTx,\nSpeedRx:       t.SpeedRx,\nWeight:        t.Weight,'
    ),
    # Patch 5: GetStatus - no leading tabs
    (
        'status.TotalBytesSent += snap.BytesSent\nstatus.TotalBytesRecv += snap.BytesRecv',
        'status.TotalBytesSent += snap.BytesSent\nstatus.TotalBytesRecv += snap.BytesRecv\nstatus.TotalSpeedTx += snap.SpeedTx\nstatus.TotalSpeedRx += snap.SpeedRx'
    ),
    # Patch 6: updateBandwidth - uses 8-space indentation (2 tabs shown as spaces)
    (
        '        if rx > 0 || tx > 0 {\n                t.mu.Lock()\n                t.BytesRecv = rx\n                t.BytesSent = tx\n                t.mu.Unlock()\n        }\n}',
        '''        now := time.Now()
        t.mu.Lock()
        if !t.LastBwTime.IsZero() {
                elapsed := now.Sub(t.LastBwTime).Seconds()
                if elapsed > 0 {
                        if tx >= t.PrevSent {
                                t.SpeedTx = float64(tx-t.PrevSent) * 8 / elapsed / 1_000_000
                        }
                        if rx >= t.PrevRecv {
                                t.SpeedRx = float64(rx-t.PrevRecv) * 8 / elapsed / 1_000_000
                        }
                }
        }
        t.BytesSent = tx
        t.BytesRecv = rx
        t.PrevSent = tx
        t.PrevRecv = rx
        t.LastBwTime = now
        t.mu.Unlock()
}'''
    ),
]

for i, (old, new) in enumerate(patches, 4):
    if old in src:
        src = src.replace(old, new)
        print(f'Patch {i} OK')
    else:
        print(f'ERROR patch {i}')
        key = old.splitlines()[0]
        for j, line in enumerate(src.splitlines()):
            if key.strip() in line:
                print(f'  line {j+1}: {repr(line)}')

open('internal/bonding/bonder.go', 'w').write(src)
print('Done.')
