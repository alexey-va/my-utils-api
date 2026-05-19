-- Replace demo workout log with real training data (local@workout).

DELETE FROM workout_entries
WHERE user_id = (SELECT id FROM users WHERE email = 'local@workout');

DELETE FROM exercises
WHERE user_id = (SELECT id FROM users WHERE email = 'local@workout');

WITH wu AS (
    SELECT id AS user_id FROM users WHERE email = 'local@workout'
),
new_exercises AS (
    INSERT INTO exercises (user_id, name, muscle_group)
    SELECT wu.user_id, e.name, e.muscle_group
    FROM wu
    CROSS JOIN (
        VALUES
            ('Жим грудь', 'chest'),
            ('Бабочка', 'chest'),
            ('Бицепс', 'arms'),
            ('Трицепс', 'arms'),
            ('Плечи', 'shoulders'),
            ('Плечи блок', 'shoulders'),
            ('Присед со штангой', 'legs'),
            ('Пулл даун', 'back')
    ) AS e (name, muscle_group)
    RETURNING id, name
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
    ne.id,
    v.performed_on,
    v.weight_kg,
    v.set_count,
    v.reps_per_set,
    v.max_reps
FROM wu
CROSS JOIN new_exercises ne
JOIN (
    VALUES
        ('Жим грудь', DATE '2026-04-23', 75, 3, 8, 7),
        ('Жим грудь', DATE '2026-05-10', 70, 3, 8, 7),
        ('Бабочка', DATE '2026-04-23', 11, 3, 10, 10),
        ('Бицепс', DATE '2026-04-26', 35, 3, 10, 10),
        ('Бицепс', DATE '2026-05-11', 35, 3, 10, 10),
        ('Бицепс', DATE '2026-05-14', 40, 3, 10, 10),
        ('Трицепс', DATE '2026-05-12', 31, 3, 10, 10),
        ('Плечи', DATE '2026-05-13', 20, 3, 10, 10),
        ('Плечи блок', DATE '2026-05-13', 3, 3, 10, 10),
        ('Присед со штангой', DATE '2026-04-23', 20, 2, 12, 12),
        ('Присед со штангой', DATE '2026-05-11', 45, 3, 15, 15),
        ('Присед со штангой', DATE '2026-05-14', 50, 3, 12, 12),
        ('Пулл даун', DATE '2026-05-10', 60, 3, 10, 10),
        ('Пулл даун', DATE '2026-05-12', 72, 3, 10, 7)
) AS v (exercise_name, performed_on, weight_kg, set_count, reps_per_set, max_reps)
    ON ne.name = v.exercise_name;
