CREATE UNIQUE INDEX idx_wireguard_relays_name_ci
    ON wireguard_relays(lower(name));
