-- Tags for runtime property grouping (synced from code definitions on startup).

ALTER TABLE app_settings
    ADD COLUMN tags JSONB NOT NULL DEFAULT '[]'::jsonb;
