ALTER TABLE users
    ADD COLUMN username VARCHAR(64),
    ADD COLUMN role VARCHAR(16) NOT NULL DEFAULT 'USER',
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users
SET username = CASE
    WHEN email = 'local@workout' THEN 'local-workout'
    WHEN email = 'dev@example.com' THEN 'dev'
    ELSE 'user-' || SUBSTRING(id::text, 1, 8)
END;

ALTER TABLE users
    ALTER COLUMN username SET NOT NULL;

CREATE UNIQUE INDEX uq_users_username_lower ON users (LOWER(username));
CREATE UNIQUE INDEX uq_users_email_lower ON users (LOWER(email));
