-- =============================================================================
-- AcaSkill VPN - Database Schema
-- =============================================================================

-- License keys table
CREATE TABLE IF NOT EXISTS licenses (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key           VARCHAR(64) UNIQUE NOT NULL,
    plan          VARCHAR(32) NOT NULL DEFAULT 'standard',
    max_devices   INT NOT NULL DEFAULT 3,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    notes         TEXT
);

-- Registered client devices
CREATE TABLE IF NOT EXISTS devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    license_id      UUID NOT NULL REFERENCES licenses(id) ON DELETE CASCADE,
    device_name     VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE
);

-- WireGuard peers (one row per interface per device)
CREATE TABLE IF NOT EXISTS wg_peers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    assigned_ip     INET UNIQUE NOT NULL,
    public_key      VARCHAR(64) UNIQUE NOT NULL,
    interface_label VARCHAR(64),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_handshake  TIMESTAMPTZ
);

-- Connection sessions for monitoring
CREATE TABLE IF NOT EXISTS sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at        TIMESTAMPTZ,
    bytes_sent      BIGINT NOT NULL DEFAULT 0,
    bytes_received  BIGINT NOT NULL DEFAULT 0,
    peer_count      INT NOT NULL DEFAULT 0
);

-- IP sequence for peer allocation (10.8.1.1 - 10.8.254.254)
CREATE SEQUENCE IF NOT EXISTS ip_last_octet_seq START 1 MAXVALUE 65534;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_licenses_key    ON licenses(key);
CREATE INDEX IF NOT EXISTS idx_devices_license ON devices(license_id);
CREATE INDEX IF NOT EXISTS idx_peers_device    ON wg_peers(device_id);
CREATE INDEX IF NOT EXISTS idx_peers_pubkey    ON wg_peers(public_key);
CREATE INDEX IF NOT EXISTS idx_sessions_device ON sessions(device_id);

-- Helper: allocate next available peer IP
CREATE OR REPLACE FUNCTION allocate_peer_ip()
RETURNS INET AS $$
DECLARE
    next_val BIGINT;
    octet3   INT;
    octet4   INT;
BEGIN
    next_val := nextval('ip_last_octet_seq');
    octet3   := ((next_val - 1) / 254) + 1;
    octet4   := ((next_val - 1) % 254) + 1;
    RETURN ('10.8.' || octet3 || '.' || octet4)::INET;
END;
$$ LANGUAGE plpgsql;

-- Seed: development test license
INSERT INTO licenses (key, plan, max_devices, notes)
VALUES ('TEST-AAAA-BBBB-CCCC-DDDD', 'pro', 10, 'Development test key')
ON CONFLICT DO NOTHING;
