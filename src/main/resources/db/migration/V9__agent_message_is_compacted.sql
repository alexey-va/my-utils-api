ALTER TABLE agent_conversation_messages
    ADD COLUMN is_compacted BOOLEAN NOT NULL DEFAULT false;

UPDATE agent_conversation_messages
SET is_compacted = true
WHERE compacted_into_summary_id IS NOT NULL;
