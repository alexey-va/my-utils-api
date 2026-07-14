CREATE TABLE health_steps (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    step_date  DATE NOT NULL,
    steps      INT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_health_steps_positive CHECK (steps >= 0),
    CONSTRAINT uq_health_steps_user_date UNIQUE (user_id, step_date)
);

CREATE INDEX idx_health_steps_user_date ON health_steps (user_id, step_date DESC);
