CREATE TABLE agent_context_summaries (
    id UUID PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    sequence INT NOT NULL,
    summary_text TEXT NOT NULL,
    covers_message_id_from BIGINT NOT NULL,
    covers_message_id_to BIGINT NOT NULL,
    source_message_count INT NOT NULL,
    model VARCHAR(200),
    tokens_before INT,
    tokens_after INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_summaries_chat_seq ON agent_context_summaries (chat_id, sequence);

ALTER TABLE agent_conversation_messages
    ADD COLUMN excluded_from_context BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN compacted_into_summary_id UUID REFERENCES agent_context_summaries (id);

CREATE INDEX idx_agent_conv_chat_created_compact ON agent_conversation_messages (chat_id, created_at);
