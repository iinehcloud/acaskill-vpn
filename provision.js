// =============================================================================
// AcaSkill VPN API - Provisioning Routes
// POST /provision/peer   → registers a WG public key, returns assigned IP
// DELETE /provision/peer → removes a peer
// =============================================================================

import { sql } from '../db.js'
import { exec } from 'child_process'
import { promisify } from 'util'
import fs from 'fs/promises'

const execAsync = promisify(exec)
const WG_CONFIG  = process.env.WG_CONFIG_PATH || '/etc/wireguard/wg0.conf'
const WG_PORT    = process.env.WG_PORT || '51820'
const SERVER_PUBKEY = process.env.WG_SERVER_PUBLIC_KEY

// ── Add peer to wg0.conf and reload live (no restart needed) ─────────────────
async function addWgPeer(publicKey, assignedIp) {
  // Live-add without restarting WireGuard
  await execAsync(`wg set wg0 peer ${publicKey} allowed-ips ${assignedIp}/32`)

  // Persist to config file for reboot survival
  const peerBlock = `
# AcaSkill Peer - ${assignedIp}
[Peer]
PublicKey  = ${publicKey}
AllowedIPs = ${assignedIp}/32
`
  await fs.appendFile(WG_CONFIG, peerBlock)
}

async function removeWgPeer(publicKey, assignedIp) {
  // Live-remove
  await execAsync(`wg set wg0 peer ${publicKey} remove`).catch(() => {})

  // Remove from config file
  const config = await fs.readFile(WG_CONFIG, 'utf8')
  const cleaned = config.replace(
    new RegExp(`\\n# AcaSkill Peer - ${assignedIp.replace('.', '\\.')}[^\\n]*\\n\\[Peer\\]\\nPublicKey  = [^\\n]+\\nAllowedIPs = [^\\n]+\\n`, 'g'),
    ''
  )
  await fs.writeFile(WG_CONFIG, cleaned)
}

export async function provisionRoutes(app) {

  // ── POST /provision/peer ────────────────────────────────────────────────────
  // Client sends its public key + interface label, server assigns an IP
  app.post('/peer', {
    config: { rateLimit: { max: 30, timeWindow: '1 minute' } },
    schema: {
      body: {
        type: 'object',
        required: ['deviceId', 'publicKey', 'licenseKey'],
        properties: {
          deviceId:       { type: 'string', format: 'uuid' },
          publicKey:      { type: 'string', minLength: 40, maxLength: 64 },
          licenseKey:     { type: 'string' },
          interfaceLabel: { type: 'string', maxLength: 64, default: 'unknown' }
        }
      }
    }
  }, async (request, reply) => {
    const { deviceId, publicKey, licenseKey, interfaceLabel } = request.body

    // Verify device belongs to valid license
    const [device] = await sql`
      SELECT d.id FROM devices d
      JOIN licenses l ON d.license_id = l.id
      WHERE d.id = ${deviceId}
        AND l.key = ${licenseKey}
        AND d.is_active = true
        AND l.is_active = true
    `

    if (!device) {
      return reply.status(403).send({ ok: false, error: 'Invalid device or license' })
    }

    // Check if this public key already has a peer
    const [existing] = await sql`
      SELECT id, assigned_ip FROM wg_peers WHERE public_key = ${publicKey}
    `

    if (existing) {
      // Return existing assignment (idempotent)
      return {
        ok: true,
        assignedIp:    existing.assigned_ip,
        serverPubKey:  SERVER_PUBKEY,
        serverPort:    parseInt(WG_PORT),
        endpoint:      `${process.env.VPN_DOMAIN || 'vpn.acaskill.com'}:${WG_PORT}`
      }
    }

    // Allocate a new IP from the pool
    const [{ allocate_peer_ip: assignedIp }] = await sql`SELECT allocate_peer_ip()`

    // Store peer in DB
    await sql`
      INSERT INTO wg_peers (device_id, assigned_ip, public_key, interface_label)
      VALUES (${deviceId}, ${assignedIp}, ${publicKey}, ${interfaceLabel || 'unknown'})
    `

    // Add to WireGuard
    await addWgPeer(publicKey, assignedIp)

    // Update device last seen
    await sql`UPDATE devices SET last_seen_at = NOW() WHERE id = ${deviceId}`

    app.log.info({ publicKey, assignedIp, interfaceLabel }, 'Peer provisioned')

    return {
      ok: true,
      assignedIp,
      serverPubKey: SERVER_PUBKEY,
      serverPort:   parseInt(WG_PORT),
      endpoint:     `${process.env.VPN_DOMAIN || 'vpn.acaskill.com'}:${WG_PORT}`
    }
  })

  // ── DELETE /provision/peer ──────────────────────────────────────────────────
  // Called when client disconnects an interface gracefully
  app.delete('/peer', {
    schema: {
      body: {
        type: 'object',
        required: ['publicKey', 'deviceId'],
        properties: {
          publicKey: { type: 'string' },
          deviceId:  { type: 'string', format: 'uuid' }
        }
      }
    }
  }, async (request, reply) => {
    const { publicKey, deviceId } = request.body

    const [peer] = await sql`
      SELECT p.id, p.assigned_ip FROM wg_peers p
      WHERE p.public_key = ${publicKey} AND p.device_id = ${deviceId}
    `

    if (!peer) {
      return reply.status(404).send({ ok: false, error: 'Peer not found' })
    }

    await removeWgPeer(publicKey, peer.assigned_ip)
    await sql`DELETE FROM wg_peers WHERE id = ${peer.id}`

    return { ok: true, message: 'Peer removed' }
  })

  // ── GET /provision/status/:deviceId ────────────────────────────────────────
  // Returns all active peers for a device (for client reconnect after restart)
  app.get('/status/:deviceId', async (request, reply) => {
    const { deviceId } = request.params

    const peers = await sql`
      SELECT assigned_ip, public_key, interface_label
      FROM wg_peers
      WHERE device_id = ${deviceId}
    `

    return { ok: true, peers }
  })
}
