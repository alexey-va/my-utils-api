ALTER TABLE wireguard_peers
    ADD COLUMN category VARCHAR(80) NOT NULL DEFAULT 'Пользовательские',
    ADD COLUMN sort_order INTEGER;

WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY relay_id ORDER BY created_at, id) - 1 AS position
    FROM wireguard_peers
)
UPDATE wireguard_peers AS peer
SET sort_order = ranked.position
FROM ranked
WHERE ranked.id = peer.id;

ALTER TABLE wireguard_peers
    ALTER COLUMN sort_order SET NOT NULL,
    ALTER COLUMN sort_order SET DEFAULT 0;

CREATE INDEX idx_wireguard_peers_relay_order
    ON wireguard_peers(relay_id, sort_order, created_at);
