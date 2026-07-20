CREATE TABLE health_body_weight (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    weight_date DATE NOT NULL,
    weight_kg   NUMERIC(5, 1) NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_health_body_weight_range CHECK (weight_kg >= 20 AND weight_kg <= 400),
    CONSTRAINT uq_health_body_weight_user_date UNIQUE (user_id, weight_date)
);

CREATE INDEX idx_health_body_weight_user_date ON health_body_weight (user_id, weight_date DESC);
