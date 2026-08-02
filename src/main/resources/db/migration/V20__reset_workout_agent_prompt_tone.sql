-- Re-seed the prompt after removing tone policing; correctness and tool safety remain enforced separately.
DELETE FROM app_settings WHERE key = 'agent.system-prompt';
