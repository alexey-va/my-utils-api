ALTER TABLE wireguard_relays
    ADD COLUMN routing_mode VARCHAR(32) NOT NULL DEFAULT 'AWG_ONLY',
    ADD COLUMN ru_prefix_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN routing_updated_at TIMESTAMPTZ,
    ADD CONSTRAINT chk_wireguard_relays_ru_prefix_count CHECK (ru_prefix_count >= 0);

CREATE TABLE wireguard_peer_metric_samples (
    id UUID PRIMARY KEY,
    peer_id UUID NOT NULL REFERENCES wireguard_peers(id) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL,
    download_bytes BIGINT NOT NULL,
    upload_bytes BIGINT NOT NULL,
    latest_handshake_at TIMESTAMPTZ,
    CONSTRAINT chk_wireguard_metric_download_bytes CHECK (download_bytes >= 0),
    CONSTRAINT chk_wireguard_metric_upload_bytes CHECK (upload_bytes >= 0)
);

CREATE INDEX idx_wireguard_peer_metric_samples_peer_recorded
    ON wireguard_peer_metric_samples(peer_id, recorded_at);

CREATE INDEX idx_wireguard_peer_metric_samples_recorded
    ON wireguard_peer_metric_samples(recorded_at);
