ALTER TABLE wireguard_relays
    ADD COLUMN routing_healthy BOOLEAN,
    ADD COLUMN routing_checked_at TIMESTAMPTZ,
    ADD COLUMN exit_health JSONB,
    ADD CONSTRAINT chk_wireguard_relays_exit_health_object
        CHECK (exit_health IS NULL OR jsonb_typeof(exit_health) = 'object');
