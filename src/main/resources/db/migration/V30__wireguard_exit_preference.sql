ALTER TABLE wireguard_relays
    ADD COLUMN exit_preference VARCHAR(16) NOT NULL DEFAULT 'AUTO',
    ADD CONSTRAINT chk_wireguard_relays_exit_preference
        CHECK (exit_preference IN ('AUTO', 'PRIMARY', 'SECONDARY'));
