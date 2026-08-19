CREATE TABLE wireguard_relays (
    id UUID PRIMARY KEY,
    name VARCHAR(80) NOT NULL UNIQUE,
    public_endpoint VARCHAR(255) NOT NULL,
    client_cidr VARCHAR(32) NOT NULL,
    client_dns VARCHAR(64) NOT NULL,
    interface_name VARCHAR(15) NOT NULL,
    agent_token_hash VARCHAR(64) NOT NULL,
    server_public_key VARCHAR(64),
    desired_revision BIGINT NOT NULL DEFAULT 0,
    applied_revision BIGINT,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE wireguard_peers (
    id UUID PRIMARY KEY,
    relay_id UUID NOT NULL REFERENCES wireguard_relays(id),
    name VARCHAR(120) NOT NULL,
    public_key VARCHAR(64) NOT NULL,
    private_key_ciphertext TEXT NOT NULL,
    private_key_nonce VARCHAR(64) NOT NULL,
    assigned_ip VARCHAR(45) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    latest_handshake_at TIMESTAMPTZ,
    raw_receive_bytes BIGINT NOT NULL DEFAULT 0,
    raw_transmit_bytes BIGINT NOT NULL DEFAULT 0,
    total_receive_bytes BIGINT NOT NULL DEFAULT 0,
    total_transmit_bytes BIGINT NOT NULL DEFAULT 0,
    metrics_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_wireguard_peer_relay_name UNIQUE (relay_id, name),
    CONSTRAINT uq_wireguard_peer_relay_public_key UNIQUE (relay_id, public_key),
    CONSTRAINT uq_wireguard_peer_relay_ip UNIQUE (relay_id, assigned_ip)
);

CREATE INDEX idx_wireguard_peers_relay_id ON wireguard_peers(relay_id);
