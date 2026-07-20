-- Re-seed agent.system-prompt from code default on next API start
-- (old seeded value still treated «70 10/12» as two sets).
DELETE FROM app_settings WHERE key = 'agent.system-prompt';
