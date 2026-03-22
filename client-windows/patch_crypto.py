content = open('internal/tun/adapter.go').read()

# 1. Add encryption to sendWrapped - encrypt payload before building packet
old_send = '''\tcopy(pkt[HeaderSize:], payload)
\tep.send(pkt)
}'''

new_send = '''\t// Encrypt payload if session key available
\tif ep.session != nil {
\t\tencrypted, err := ep.session.Encrypt(payload)
\t\tif err == nil {
\t\t\tpkt = make([]byte, HeaderSize+len(encrypted))
\t\t\tbinary.BigEndian.PutUint32(pkt[0:4], MagicClient)
\t\t\tcopy(pkt[4:20], a.deviceID[:])
\t\t\tbinary.BigEndian.PutUint64(pkt[20:28], seq)
\t\t\tbinary.BigEndian.PutUint16(pkt[28:30], tunIdx)
\t\t\tbinary.BigEndian.PutUint16(pkt[30:32], uint16(len(encrypted)))
\t\t\tcopy(pkt[HeaderSize:], encrypted)
\t\t}
\t} else {
\t\tcopy(pkt[HeaderSize:], payload)
\t}
\tep.send(pkt)
}'''

if old_send in content:
    content = content.replace(old_send, new_send)
    print("Added encryption to sendWrapped")
else:
    print("ERROR: sendWrapped pattern not found")
    idx = content.find('copy(pkt[HeaderSize:], payload)')
    print(repr(content[idx-10:idx+60]))

# 2. Add decryption to recvLoop - decrypt after magic check
old_recv = '''\t\tpkt := make([]byte, payloadLen)
\t\tcopy(pkt, buf[RetHeaderSize:RetHeaderSize+payloadLen])

\t\tep.mu.Lock()
\t\tep.lastSeen = time.Now()
\t\tep.mu.Unlock()

\t\tselect {
\t\tcase a.retBuf <- pkt:
\t\tdefault:
\t\t}'''

new_recv = '''\t\trawPayload := buf[RetHeaderSize:RetHeaderSize+payloadLen]

\t\tep.mu.Lock()
\t\tsess := ep.session
\t\tep.lastSeen = time.Now()
\t\tep.mu.Unlock()

\t\tvar pkt []byte
\t\tif sess != nil {
\t\t\tdecrypted, err := sess.Decrypt(rawPayload)
\t\t\tif err != nil {
\t\t\t\tcontinue // drop invalid packet
\t\t\t}
\t\t\tpkt = decrypted
\t\t} else {
\t\t\tpkt = make([]byte, payloadLen)
\t\t\tcopy(pkt, rawPayload)
\t\t}

\t\tselect {
\t\tcase a.retBuf <- pkt:
\t\tdefault:
\t\t}'''

if old_recv in content:
    content = content.replace(old_recv, new_recv)
    print("Added decryption to recvLoop")
else:
    print("ERROR: recvLoop pattern not found")
    idx = content.find('retBuf <- pkt')
    print(repr(content[idx-200:idx+50]))

open('internal/tun/adapter.go', 'w').write(content)
print('Done')
