CREATE TABLE agent_conversation_messages (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    message_json TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_conv_chat_created ON agent_conversation_messages (chat_id, created_at);

CREATE TABLE agent_user_facts (
    id UUID PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_facts_chat ON agent_user_facts (chat_id);
