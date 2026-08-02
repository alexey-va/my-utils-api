CREATE TABLE agent_test_sandbox_states (
    memory_chat_id BIGINT PRIMARY KEY
        REFERENCES agent_test_chats(memory_chat_id) ON DELETE CASCADE,
    state_json TEXT NOT NULL DEFAULT '{}',
    version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO agent_test_sandbox_states (memory_chat_id)
SELECT memory_chat_id
FROM agent_test_chats;

ALTER TABLE agent_test_chats
    DROP COLUMN user_context_chat_id;
