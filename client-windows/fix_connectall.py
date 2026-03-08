path = r'internal\ipc\server.go'
src = open(path).read()

old = '''for _, iface := range ifaces {
if iface.IsConnected {
bonder.ConnectInterface(iface)
}
}'''

new = '''for _, iface := range ifaces {
if !iface.IsConnected {
bonder.ConnectInterface(iface)
}
}'''

if old in src:
    src = src.replace(old, new)
    open(path, 'w').write(src)
    print('Fix ConnectAll: OK')
else:
    print('ERROR: pattern not found')
