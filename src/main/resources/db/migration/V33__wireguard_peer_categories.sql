CREATE TABLE wireguard_peer_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    relay_id UUID NOT NULL REFERENCES wireguard_relays(id) ON DELETE CASCADE,
    name VARCHAR(80) NOT NULL CHECK (btrim(name) <> ''),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_wireguard_peer_categories_relay_name
    ON wireguard_peer_categories(relay_id, lower(name));
CREATE INDEX idx_wireguard_peer_categories_relay_order
    ON wireguard_peer_categories(relay_id, sort_order, created_at);

INSERT INTO wireguard_peer_categories(relay_id, name, sort_order)
SELECT relay.id, defaults.name, defaults.sort_order
FROM wireguard_relays AS relay
CROSS JOIN (VALUES
    ('Пользовательские', 0),
    ('Служебные', 1)
) AS defaults(name, sort_order);

WITH distinct_categories AS (
    SELECT
        relay_id,
        lower(btrim(category)) AS category_key,
        min(btrim(category)) AS name,
        min(sort_order) AS first_peer_order
    FROM wireguard_peers
    WHERE btrim(category) <> ''
      AND lower(btrim(category)) NOT IN (lower('Пользовательские'), lower('Служебные'))
    GROUP BY relay_id, lower(btrim(category))
), ranked_categories AS (
    SELECT
        relay_id,
        name,
        1 + row_number() OVER (
            PARTITION BY relay_id
            ORDER BY first_peer_order, category_key
        ) AS sort_order
    FROM distinct_categories
)
INSERT INTO wireguard_peer_categories(relay_id, name, sort_order)
SELECT relay_id, name, sort_order
FROM ranked_categories
ON CONFLICT DO NOTHING;

UPDATE wireguard_peers AS peer
SET category = category.name
FROM wireguard_peer_categories AS category
WHERE category.relay_id = peer.relay_id
  AND lower(category.name) = lower(btrim(peer.category))
  AND peer.category <> category.name;
