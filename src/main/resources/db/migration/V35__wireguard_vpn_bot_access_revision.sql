ALTER TABLE wireguard_vpn_bot_users
    ADD COLUMN access_revision BIGINT NOT NULL DEFAULT 1
        CHECK (access_revision > 0);
