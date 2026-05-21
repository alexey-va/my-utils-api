-- Runtime settings (editable via /api/admin/settings while app is running).

CREATE TABLE app_settings (
    key         VARCHAR(128) PRIMARY KEY,
    value       JSONB        NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255)
);

INSERT INTO app_settings (key, value) VALUES
    ('temporal.evening-reminder.enabled', 'false'::jsonb),
    ('temporal.evening-reminder.hour', '20'::jsonb),
    ('temporal.evening-reminder.minute', '0'::jsonb),
    ('temporal.zone-id', '"Europe/Moscow"'::jsonb),
    ('openrouter.model', '"anthropic/claude-3.5-haiku"'::jsonb),
    ('openrouter.max-tool-iterations', '8'::jsonb),
    ('telegram.conversation-ttl-hours', '48'::jsonb);
