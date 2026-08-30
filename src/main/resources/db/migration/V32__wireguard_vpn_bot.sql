CREATE TABLE wireguard_vpn_bot_users (
    telegram_user_id BIGINT PRIMARY KEY CHECK (telegram_user_id > 0),
    chat_id BIGINT NOT NULL,
    username VARCHAR(64) NOT NULL DEFAULT '',
    display_name VARCHAR(160) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'BLOCKED')),
    peer_limit INTEGER NOT NULL DEFAULT 1 CHECK (peer_limit BETWEEN 1 AND 10),
    approved_by BIGINT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wireguard_vpn_bot_users_status
    ON wireguard_vpn_bot_users(status, requested_at, telegram_user_id);

CREATE TABLE wireguard_vpn_bot_peer_owners (
    peer_id UUID PRIMARY KEY REFERENCES wireguard_peers(id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL REFERENCES wireguard_vpn_bot_users(telegram_user_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wireguard_vpn_bot_peer_owners_user
    ON wireguard_vpn_bot_peer_owners(telegram_user_id, created_at, peer_id);

CREATE TABLE wireguard_vpn_bot_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_telegram_user_id BIGINT NOT NULL,
    target_telegram_user_id BIGINT NOT NULL,
    action VARCHAR(48) NOT NULL,
    peer_id UUID,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wireguard_vpn_bot_audit_target
    ON wireguard_vpn_bot_audit_events(target_telegram_user_id, created_at DESC);
