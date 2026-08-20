CREATE TABLE wireguard_exit_health_samples (
    id UUID PRIMARY KEY,
    relay_id UUID NOT NULL REFERENCES wireguard_relays(id) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL,
    overall_status VARCHAR(16) NOT NULL,
    active_exit VARCHAR(16),
    primary_healthy BOOLEAN NOT NULL,
    primary_latency_ms DOUBLE PRECISION,
    primary_failure_reason VARCHAR(80),
    secondary_healthy BOOLEAN NOT NULL,
    secondary_latency_ms DOUBLE PRECISION,
    secondary_failure_reason VARCHAR(80),
    CONSTRAINT chk_wireguard_exit_health_status
        CHECK (overall_status IN ('HEALTHY', 'DEGRADED', 'DOWN')),
    CONSTRAINT chk_wireguard_exit_health_active
        CHECK (active_exit IS NULL OR active_exit IN ('primary', 'secondary')),
    CONSTRAINT chk_wireguard_exit_health_primary_latency
        CHECK (primary_latency_ms IS NULL OR primary_latency_ms >= 0),
    CONSTRAINT chk_wireguard_exit_health_secondary_latency
        CHECK (secondary_latency_ms IS NULL OR secondary_latency_ms >= 0)
);

CREATE INDEX idx_wireguard_exit_health_samples_relay_recorded
    ON wireguard_exit_health_samples(relay_id, recorded_at);
