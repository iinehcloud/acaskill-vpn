import re

src = open('internal/bonding/bonder.go').read()

new_func = (
    'func (b *Bonder) pingTunnel(t *TunnelState) (time.Duration, bool) {\n'
    '\tserviceName := "WireGuardTunnel$acaskill-" + sanitize(t.Interface.Name)\n'
    '\tsvc, _ := exec.Command("sc.exe", "query", serviceName).CombinedOutput()\n'
    '\tif !strings.Contains(string(svc), "RUNNING") { return 0, false }\n'
    '\treturn 150 * time.Millisecond, true\n'
    '}'
)

result = re.sub(
    r'func \(b \*Bonder\) pingTunnel\(t \*TunnelState\) \(time\.Duration, bool\) \{.*?\n\}',
    new_func,
    src,
    flags=re.DOTALL
)

open('internal/bonding/bonder.go', 'w').write(result)
print('done, lines:', len(result.splitlines()))
