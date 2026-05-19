CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users (email);

CREATE TABLE exercises (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_exercises_user_name UNIQUE (user_id, name)
);

CREATE TABLE workout_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    exercise_id  UUID NOT NULL REFERENCES exercises (id) ON DELETE CASCADE,
    performed_on DATE NOT NULL,
    weight_kg    INT NOT NULL,
    set_count    INT NOT NULL,
    reps_per_set INT NOT NULL,
    max_reps     INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_workout_weight CHECK (weight_kg > 0),
    CONSTRAINT chk_workout_sets CHECK (set_count >= 1),
    CONSTRAINT chk_workout_reps CHECK (reps_per_set >= 1),
    CONSTRAINT chk_workout_max_reps CHECK (max_reps >= 1),
    CONSTRAINT uq_workout_user_exercise_date UNIQUE (user_id, exercise_id, performed_on)
);

CREATE INDEX idx_workout_entries_user_date ON workout_entries (user_id, performed_on DESC, created_at DESC);
CREATE INDEX idx_workout_entries_exercise ON workout_entries (exercise_id, performed_on DESC);

-- dev@example.com / password
INSERT INTO users (email, password_hash)
VALUES (
    'dev@example.com',
    '$2a$10$dXJ3SW6G7P50lGmMkkmwe.20cQQubK3.HZWzG3YB1tlRy.fqvM/BG'
),
(
    'local@workout',
    '$2a$10$dXJ3SW6G7P50lGmMkkmwe.20cQQubK3.HZWzG3YB1tlRy.fqvM/BG'
);

INSERT INTO exercises (user_id, name)
SELECT wu.id, names.name
FROM users wu
CROSS JOIN (
    VALUES
        ('Bench press'),
        ('Squat'),
        ('Deadlift'),
        ('Shoulder press'),
        ('Barbell row'),
        ('Pull-up')
) AS names (name)
WHERE wu.email = 'local@workout';

WITH workout_user AS (
    SELECT id AS user_id
    FROM users
    WHERE email = 'local@workout'
),
training_days AS (
    SELECT
        (CURRENT_DATE - day_offset) AS performed_on,
        day_offset,
        (day_offset / 2) % 3 AS session_kind
    FROM generate_series(0, 55, 2) AS day_offset
),
session_exercises AS (
    SELECT
        td.performed_on,
        td.day_offset,
        td.session_kind,
        plan.exercise_name,
        plan.weight_base,
        plan.weight_step,
        plan.set_count,
        plan.reps_per_set,
        plan.max_bonus
    FROM training_days td
    CROSS JOIN LATERAL (
        VALUES
            (0, 'Bench press', 60, 2, 3, 8, 2),
            (0, 'Shoulder press', 22, 1, 3, 10, 2),
            (0, 'Barbell row', 50, 2, 3, 10, 2),
            (0, 'Pull-up', 8, 1, 4, 6, 2),
            (1, 'Squat', 85, 3, 3, 6, 1),
            (1, 'Deadlift', 95, 3, 3, 5, 1),
            (1, 'Bench press', 52, 1, 3, 10, 2),
            (1, 'Barbell row', 48, 2, 3, 10, 2),
            (2, 'Squat', 80, 2, 3, 8, 2),
            (2, 'Deadlift', 100, 3, 3, 5, 1),
            (2, 'Shoulder press', 20, 1, 3, 10, 2),
            (2, 'Pull-up', 6, 1, 4, 7, 2)
    ) AS plan (
        kind,
        exercise_name,
        weight_base,
        weight_step,
        set_count,
        reps_per_set,
        max_bonus
    )
    WHERE plan.kind = td.session_kind
)
INSERT INTO workout_entries (
    user_id,
    exercise_id,
    performed_on,
    weight_kg,
    set_count,
    reps_per_set,
    max_reps
)
SELECT
    wu.user_id,
    e.id,
    se.performed_on,
    GREATEST(
        1,
        se.weight_base
        + ((55 - se.day_offset) / 7) * se.weight_step
        + CASE
            WHEN se.exercise_name = 'Bench press' AND se.session_kind = 1 THEN -8
            ELSE 0
        END
    )::INT,
    se.set_count,
    se.reps_per_set,
    se.reps_per_set + se.max_bonus
FROM workout_user wu
JOIN session_exercises se ON TRUE
JOIN exercises e ON e.user_id = wu.user_id AND e.name = se.exercise_name;
