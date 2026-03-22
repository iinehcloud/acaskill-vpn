import { sql } from '../db.js'
import { exec } from 'child_process'
import { promisify } from 'util'
import fs from 'fs/promises'
import { randomBytes } from 'crypto'

const execAsync = promisify(exec)
const WG_CONFIG     = process.env.WG_CONFIG_PATH || '/etc/wireguard/wg0.conf'
const WG_PORT       = parseInt(process.env.WG_PORT || '51820')
const SERVER_PUBKEY = process.env.WG_SERVER_PUBLIC_KEY
const VPN_DOMAIN    = process.env.VPN_DOMAIN || 'vpn.acaskill.com'

async function wg(...args) {
  const cmd = 'nsenter --net=/proc/1/ns/net wg ' + args.join(' ')
  const { stdout } = await execAsync(cmd)
  return stdout.trim()
}

async function addWgPeer(publicKey, assignedIp) {
  await wg('set', 'wg0', 'peer', publicKey, 'allowed-ips', assignedIp + '/32')
  const config = await fs.readFile(WG_CONFIG, 'utf8')
  if (!config.includes(publicKey)) {
    await fs.appendFile(WG_CONFIG, '\n[Peer]\nPublicKey = ' + publicKey + '\nAllowedIPs = ' + assignedIp + '/32\n')
  }
}

async function removeWgPeer(publicKey) {
  try { await wg('set', 'wg0', 'peer', publicKey, 'remove') } catch(e) {}
  const config = await fs.readFile(WG_CONFIG, 'utf8')
  const lines = config.split('\n')
  const filtered = []
  let skip = false
  for (const line of lines) {
    if (line === '[Peer]') { skip = false }
    if (line.startsWith('PublicKey') && line.includes(publicKey)) { skip = true; filtered.pop() }
    if (!skip) filtered.push(line)
    if (skip && line === '') { skip = false }
  }
  await fs.writeFile(WG_CONFIG, filtered.join('\n'))
}

export async function provisionRoutes(app) {
  app.post('/peer', {
    config: { rateLimit: { max: 30, timeWindow: '1 minute' } },
    schema: { body: { type: 'object', required: ['deviceId','publicKey','licenseKey'], properties: {
      deviceId:       { type: 'string', format: 'uuid' },
      publicKey:      { type: 'string', minLength: 40, maxLength: 64 },
      licenseKey:     { type: 'string' },
      interfaceLabel: { type: 'string', maxLength: 64, default: 'unknown' }
    }}}
  }, async (request, reply) => {
    const { deviceId, publicKey, licenseKey, interfaceLabel } = request.body
    const [device] = await sql`SELECT d.id FROM devices d JOIN licenses l ON d.license_id = l.id WHERE d.id = ${deviceId} AND l.key = ${licenseKey} AND d.is_active = true AND l.is_active = true`
    if (!device) return reply.status(403).send({ ok: false, error: 'Invalid device or license' })
    const [existing] = await sql`SELECT assigned_ip FROM wg_peers WHERE public_key = ${publicKey}`
    if (existing) {
      await addWgPeer(publicKey, existing.assigned_ip)
      let sessionKey = existing.session_key
      if (!sessionKey) {
        sessionKey = randomBytes(32).toString('hex')
        await sql`UPDATE wg_peers SET session_key = ${sessionKey} WHERE public_key = ${publicKey}`
      }
      await notifyAggregator(deviceId, existing.assigned_ip, interfaceLabel || 'unknown', sessionKey)
      return { ok: true, assignedIp: existing.assigned_ip, serverPubKey: SERVER_PUBKEY, serverPort: WG_PORT, endpoint: VPN_DOMAIN + ':' + WG_PORT, sessionKey }
    }
    const [{ allocate_peer_ip: assignedIp }] = await sql`SELECT allocate_peer_ip()`
    const sessionKey = randomBytes(32).toString('hex')
    await sql`INSERT INTO wg_peers (device_id, assigned_ip, public_key, interface_label, session_key) VALUES (${deviceId}, ${assignedIp}, ${publicKey}, ${interfaceLabel || 'unknown'}, ${sessionKey})`
    await addWgPeer(publicKey, assignedIp)
    await sql`UPDATE devices SET last_seen_at = NOW() WHERE id = ${deviceId}`
    app.log.info({ publicKey, assignedIp, interfaceLabel }, 'Peer provisioned')
    await notifyAggregator(deviceId, assignedIp, interfaceLabel || 'unknown', sessionKey)
    return { ok: true, assignedIp, serverPubKey: SERVER_PUBKEY, serverPort: WG_PORT, endpoint: VPN_DOMAIN + ':' + WG_PORT, sessionKey }
  })

  app.delete('/peer', {
    schema: { body: { type: 'object', required: ['publicKey','deviceId'], properties: { publicKey: { type: 'string' }, deviceId: { type: 'string', format: 'uuid' } } } }
  }, async (request, reply) => {
    const { publicKey, deviceId } = request.body
    const [peer] = await sql`SELECT id FROM wg_peers WHERE public_key = ${publicKey} AND device_id = ${deviceId}`
    if (!peer) return reply.status(404).send({ ok: false, error: 'Peer not found' })
    await removeWgPeer(publicKey)
    await sql`DELETE FROM wg_peers WHERE id = ${peer.id}`
    return { ok: true }
  })

  app.get('/status/:deviceId', async (request) => {
    const { deviceId } = request.params
    const peers = await sql`SELECT assigned_ip, public_key, interface_label FROM wg_peers WHERE device_id = ${deviceId}`
    return { ok: true, peers }
  })

  app.post('/heartbeat', async (request) => {
    const { ip } = request.body || {}
    if (ip) await sql`UPDATE wg_peers SET last_seen_at = NOW() WHERE assigned_ip = ${ip}`
    return { ok: true }
  })
}

async function notifyAggregator(deviceId, ip, label, sessionKey) {
  try {
    const res = await fetch('http://172.20.0.1:7878/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-API-Secret': process.env.API_SECRET },
      body: JSON.stringify({ deviceId, ip, label, sessionKey })
    })
    if (!res.ok) throw new Error(await res.text())
  } catch(e) {
    console.error('[provision] aggregator notify failed:', e.message)
  }
}
