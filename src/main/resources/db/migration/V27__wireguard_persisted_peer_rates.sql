ALTER TABLE wireguard_peers
    ADD COLUMN current_download_bytes_per_second DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN current_upload_bytes_per_second DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN raw_ru_download_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN raw_ru_upload_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN raw_non_ru_download_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN raw_non_ru_upload_bytes BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_wireguard_peer_download_rate
        CHECK (current_download_bytes_per_second >= 0),
    ADD CONSTRAINT chk_wireguard_peer_upload_rate
        CHECK (current_upload_bytes_per_second >= 0),
    ADD CONSTRAINT chk_wireguard_peer_raw_ru_download CHECK (raw_ru_download_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_peer_raw_ru_upload CHECK (raw_ru_upload_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_peer_raw_non_ru_download CHECK (raw_non_ru_download_bytes >= 0),
    ADD CONSTRAINT chk_wireguard_peer_raw_non_ru_upload CHECK (raw_non_ru_upload_bytes >= 0);

WITH latest_route_counters AS (
    SELECT DISTINCT ON (peer_id)
        peer_id,
        ru_download_bytes,
        ru_upload_bytes,
        non_ru_download_bytes,
        non_ru_upload_bytes
    FROM wireguard_peer_metric_samples
    ORDER BY peer_id, recorded_at DESC, id DESC
)
UPDATE wireguard_peers AS peer
SET raw_ru_download_bytes = latest.ru_download_bytes,
    raw_ru_upload_bytes = latest.ru_upload_bytes,
    raw_non_ru_download_bytes = latest.non_ru_download_bytes,
    raw_non_ru_upload_bytes = latest.non_ru_upload_bytes
FROM latest_route_counters AS latest
WHERE peer.id = latest.peer_id;

WITH ordered_route_counters AS (
    SELECT
        id,
        ru_download_bytes,
        ru_upload_bytes,
        non_ru_download_bytes,
        non_ru_upload_bytes,
        LAG(ru_download_bytes) OVER peer_history AS previous_ru_download_bytes,
        LAG(ru_upload_bytes) OVER peer_history AS previous_ru_upload_bytes,
        LAG(non_ru_download_bytes) OVER peer_history AS previous_non_ru_download_bytes,
        LAG(non_ru_upload_bytes) OVER peer_history AS previous_non_ru_upload_bytes
    FROM wireguard_peer_metric_samples
    WINDOW peer_history AS (PARTITION BY peer_id ORDER BY recorded_at, id)
)
UPDATE wireguard_peer_metric_samples AS sample
SET ru_download_bytes = CASE
        WHEN ordered.previous_ru_download_bytes IS NULL THEN 0
        WHEN ordered.ru_download_bytes >= ordered.previous_ru_download_bytes
            THEN ordered.ru_download_bytes - ordered.previous_ru_download_bytes
        ELSE ordered.ru_download_bytes
    END,
    ru_upload_bytes = CASE
        WHEN ordered.previous_ru_upload_bytes IS NULL THEN 0
        WHEN ordered.ru_upload_bytes >= ordered.previous_ru_upload_bytes
            THEN ordered.ru_upload_bytes - ordered.previous_ru_upload_bytes
        ELSE ordered.ru_upload_bytes
    END,
    non_ru_download_bytes = CASE
        WHEN ordered.previous_non_ru_download_bytes IS NULL THEN 0
        WHEN ordered.non_ru_download_bytes >= ordered.previous_non_ru_download_bytes
            THEN ordered.non_ru_download_bytes - ordered.previous_non_ru_download_bytes
        ELSE ordered.non_ru_download_bytes
    END,
    non_ru_upload_bytes = CASE
        WHEN ordered.previous_non_ru_upload_bytes IS NULL THEN 0
        WHEN ordered.non_ru_upload_bytes >= ordered.previous_non_ru_upload_bytes
            THEN ordered.non_ru_upload_bytes - ordered.previous_non_ru_upload_bytes
        ELSE ordered.non_ru_upload_bytes
    END
FROM ordered_route_counters AS ordered
WHERE sample.id = ordered.id;
