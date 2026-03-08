path = r'internal\ipc\server.go'
src = open(path).read()

# Remove the IsConnected check entirely - just try to connect all, 
# ConnectInterface already skips already-connected tunnels internally
old = '''for _, iface := range ifaces {
if !iface.IsConnected {
bonder.ConnectInterface(iface)
}
}'''

new = '''for _, iface := range ifaces {
bonder.ConnectInterface(iface)
}'''

if old in src:
    src = src.replace(old, new)
    open(path, 'w').write(src)
    print('Fix ConnectAll (remove IsConnected guard): OK')
else:
    print('ERROR: pattern not found')
    # Show current state
    idx = src.find('MsgConnectAll')
    print(repr(src[idx:idx+300]))
