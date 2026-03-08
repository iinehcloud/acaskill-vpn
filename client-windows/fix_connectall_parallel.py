import re

path = r'internal\ipc\server.go'
src = open(path).read()

old = '''case MsgConnectAll:
ifaces, err := bonder.GetAvailableInterfaces()
if err != nil {
return errResp(req.ID, err.Error())
}
for _, iface := range ifaces {
bonder.ConnectInterface(iface)
}
return okResp(req.ID)'''

new = '''case MsgConnectAll:
ifaces, err := bonder.GetAvailableInterfaces()
if err != nil {
return errResp(req.ID, err.Error())
}
var wg sync.WaitGroup
for _, iface := range ifaces {
wg.Add(1)
go func(i interfaces.NetworkInterface) {
defer wg.Done()
if err := bonder.ConnectInterface(i); err != nil {
log.Printf("[ipc] connect %s: %v", i.FriendlyName, err)
}
}(iface)
}
wg.Wait()
return okResp(req.ID)'''

if old in src:
    src = src.replace(old, new)
    # Add sync import if missing
    if '"sync"' not in src:
        src = src.replace('"time"', '"sync"\n\t"time"')
        print('Added sync import')
    open(path, 'w').write(src)
    print('Fix ConnectAll parallel: OK')
else:
    print('ERROR: pattern not found')
    idx = src.find('MsgConnectAll')
    print(repr(src[idx:idx+300]))
