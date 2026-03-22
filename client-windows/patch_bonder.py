content = open('internal/bonding/bonder.go').read()

idx = content.find('physIP := t.Interface.IP.String()')
print(repr(content[idx-5:idx+80]))

# Try to find and replace
old = 'physIP := t.Interface.IP.String()\nt.mu.Unlock()'
new = 'physIP := t.Interface.IP.String()\nsessionKey := ""\nif t.TunnelCfg != nil { sessionKey = t.TunnelCfg.SessionKey }\nt.mu.Unlock()'

if old in content:
    content = content.replace(old, new)
    print('Fixed')
else:
    # try with different newlines
    for sep in ['\r\n', '\n']:
        old2 = ('physIP := t.Interface.IP.String()' + sep + 't.mu.Unlock()')
        if old2 in content:
            new2 = ('physIP := t.Interface.IP.String()' + sep + 
                    'sessionKey := ""' + sep +
                    'if t.TunnelCfg != nil { sessionKey = t.TunnelCfg.SessionKey }' + sep +
                    't.mu.Unlock()')
            content = content.replace(old2, new2)
            print('Fixed with sep: ' + repr(sep))
            break
    else:
        print('ERROR: pattern not found')

open('internal/bonding/bonder.go', 'w').write(content)
