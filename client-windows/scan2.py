path = r'internal\bonding\bonder.go'
src = open(path).read()
idx = src.find('func (b *Bonder) ConnectInterface')
print(repr(src[idx:idx+300]))
