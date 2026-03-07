# This script adds bandwidth tracking to bonder.go
# It reads WireGuard stats via `wg show <tunnel> transfer`
# and updates BytesSent/BytesRecv on each TunnelState

src = open('internal/bonding/bonder.go').read()

# 1. Add bandwidth reading to doHeartbeat - after sendHeartbeat call
old = '				b.sendHeartbeat(t.AssignedIP, latency)'
new = '''				b.sendHeartbeat(t.AssignedIP, latency)
				b.updateBandwidth(t)'''
src = src.replace(old, new)

# 2. Add updateBandwidth function before rebalanceWeights
old = 'func (b *Bonder) rebalanceWeights() {'
new = '''func (b *Bonder) updateBandwidth(t *TunnelState) {
	tunnelName := "acaskill-" + sanitize(t.Interface.Name)
	// wg show <tunnel> transfer returns: <rx_bytes>\t<tx_bytes>
	out, err := exec.Command("wg", "show", tunnelName, "transfer").CombinedOutput()
	if err != nil {
		return
	}
	var rx, tx uint64
	line := strings.TrimSpace(string(out))
	// Output format: "<pubkey>\t<rx>\t<tx>" - take last two fields
	fields := strings.Fields(line)
	if len(fields) >= 3 {
		fmt.Sscanf(fields[len(fields)-2], "%d", &rx)
		fmt.Sscanf(fields[len(fields)-1], "%d", &tx)
	} else if len(fields) == 2 {
		fmt.Sscanf(fields[0], "%d", &rx)
		fmt.Sscanf(fields[1], "%d", &tx)
	}
	if rx > 0 || tx > 0 {
		t.mu.Lock()
		t.BytesRecv = rx
		t.BytesSent = tx
		t.mu.Unlock()
	}
}

func (b *Bonder) rebalanceWeights() {'''
src = src.replace('func (b *Bonder) rebalanceWeights() {', new)

# 3. Add "fmt" to imports if not present
if '"fmt"' not in src:
    src = src.replace('"context"', '"context"\n\t"fmt"')

open('internal/bonding/bonder.go', 'w').write(src)
print('done, lines:', len(src.splitlines()))
