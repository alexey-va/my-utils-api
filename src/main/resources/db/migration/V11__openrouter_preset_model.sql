UPDATE app_settings
SET value = '"@preset/deepseek"'::jsonb,
    updated_at = NOW()
WHERE key = 'openrouter.model';
