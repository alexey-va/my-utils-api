-- Re-seed the Workout agent prompt with explicit calendar and read-only rules.
-- Production keeps runtime settings across deployments, so a code-default change alone is insufficient.
DELETE FROM app_settings WHERE key = 'agent.system-prompt';
