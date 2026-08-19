ALTER TABLE wireguard_relays
    ADD COLUMN direct_probe_target VARCHAR(64),
    ADD COLUMN direct_packet_loss_percent DOUBLE PRECISION,
    ADD COLUMN direct_average_rtt_ms DOUBLE PRECISION,
    ADD COLUMN veesp_probe_target VARCHAR(64),
    ADD COLUMN veesp_packet_loss_percent DOUBLE PRECISION,
    ADD COLUMN veesp_average_rtt_ms DOUBLE PRECISION,
    ADD COLUMN route_quality_updated_at TIMESTAMPTZ,
    ADD CONSTRAINT chk_wireguard_relays_direct_loss
        CHECK (direct_packet_loss_percent IS NULL OR direct_packet_loss_percent BETWEEN 0 AND 100),
    ADD CONSTRAINT chk_wireguard_relays_direct_rtt
        CHECK (direct_average_rtt_ms IS NULL OR direct_average_rtt_ms >= 0),
    ADD CONSTRAINT chk_wireguard_relays_veesp_loss
        CHECK (veesp_packet_loss_percent IS NULL OR veesp_packet_loss_percent BETWEEN 0 AND 100),
    ADD CONSTRAINT chk_wireguard_relays_veesp_rtt
        CHECK (veesp_average_rtt_ms IS NULL OR veesp_average_rtt_ms >= 0);

ALTER TABLE wireguard_peer_metric_samples
    ADD COLUMN ru_download_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN ru_upload_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN non_ru_download_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN non_ru_upload_bytes BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_wireguard_metric_ru_download_bytes CHECK (ru_download_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_metric_ru_upload_bytes CHECK (ru_upload_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_metric_non_ru_download_bytes CHECK (non_ru_download_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_metric_non_ru_upload_bytes CHECK (non_ru_upload_bytes >= 0);
