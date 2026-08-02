CREATE SEQUENCE agent_test_chat_memory_id_seq
    AS BIGINT
    START WITH -9000000000000000
    INCREMENT BY 1
    MINVALUE -9000000000000000
    MAXVALUE -8000000000000000
    NO CYCLE;

CREATE TABLE agent_test_chats (
    id UUID PRIMARY KEY,
    memory_chat_id BIGINT NOT NULL UNIQUE,
    user_context_chat_id BIGINT NOT NULL,
    title VARCHAR(120) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_test_chats_updated_at
    ON agent_test_chats (updated_at DESC);
