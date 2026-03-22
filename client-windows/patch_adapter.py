content = open('internal/tun/adapter.go').read()

# Add session field to struct (after TunnelIP)
old_struct = 'TunnelIP   string\n\tServerAddr *net.UDPAddr'
new_struct = 'TunnelIP   string\n\tsession    *crypto.Session\n\tServerAddr *net.UDPAddr'
if old_struct in content:
    content = content.replace(old_struct, new_struct)
    print("Added session field to struct")
else:
    print("ERROR: struct pattern not found")

# Find ep creation and print exact bytes for debugging
idx = content.find('ep := &Endpoint{')
if idx >= 0:
    snippet = content[idx:idx+250]
    print("Found ep creation:")
    print(repr(snippet))
    
    # Build the exact old string from what we see
    old_ep = snippet[:snippet.find('}')+1]
    new_ep = 'var sess *crypto.Session\n\tif sessionKeyHex != "" {\n\t\tif s, err2 := crypto.NewSession(sessionKeyHex); err2 == nil {\n\t\t\tsess = s\n\t\t} else {\n\t\t\tlog.Printf("[bond] crypto init failed for %s: %v", label, err2)\n\t\t}\n\t}\n\t' + old_ep.rstrip('}') + '\t\tsession:    sess,\n\t\t}'
    content = content.replace(old_ep, new_ep)
    print("Updated ep creation")
else:
    print("ERROR: ep creation not found")

open('internal/tun/adapter.go', 'w').write(content)
print('Done')
