UPDATE app_settings
SET value = '"openai/gpt-5.4-mini"'::jsonb,
    updated_at = NOW()
WHERE key = 'openrouter.model';
