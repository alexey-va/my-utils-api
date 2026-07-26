-- Keep one rolling summary per chat. Previous auto-compaction could race and
-- create duplicate/orphan rows for the same message range.

CREATE TEMP TABLE active_agent_context_summaries AS
SELECT s.*
FROM agent_context_summaries s
WHERE EXISTS (
    SELECT 1
    FROM agent_conversation_messages m
    WHERE m.compacted_into_summary_id = s.id
);

CREATE TEMP TABLE agent_context_summary_rollups AS
WITH summary_rollup AS (
    SELECT
        chat_id,
        (ARRAY_AGG(id ORDER BY sequence DESC, created_at DESC))[1] AS canonical_id,
        STRING_AGG(summary_text, E'\n\n---\n\n' ORDER BY sequence, created_at) AS summary_text
    FROM active_agent_context_summaries
    GROUP BY chat_id
),
message_stats AS (
    SELECT
        chat_id,
        MIN(id) AS covers_message_id_from,
        MAX(id) AS covers_message_id_to,
        COUNT(*)::INT AS source_message_count,
        SUM(LENGTH(message_json))::INT AS tokens_before
    FROM agent_conversation_messages
    WHERE compacted_into_summary_id IS NOT NULL
    GROUP BY chat_id
)
SELECT
    summaries.chat_id,
    summaries.canonical_id,
    summaries.summary_text,
    messages.covers_message_id_from,
    messages.covers_message_id_to,
    messages.source_message_count,
    messages.tokens_before
FROM summary_rollup summaries
JOIN message_stats messages USING (chat_id);

UPDATE agent_context_summaries summary
SET
    sequence = 1,
    summary_text = rollup.summary_text,
    covers_message_id_from = rollup.covers_message_id_from,
    covers_message_id_to = rollup.covers_message_id_to,
    source_message_count = rollup.source_message_count,
    tokens_before = rollup.tokens_before,
    tokens_after = LENGTH(rollup.summary_text)
FROM agent_context_summary_rollups rollup
WHERE summary.id = rollup.canonical_id;

UPDATE agent_conversation_messages message
SET
    compacted_into_summary_id = rollup.canonical_id,
    is_compacted = TRUE
FROM agent_context_summary_rollups rollup
WHERE message.chat_id = rollup.chat_id
  AND message.compacted_into_summary_id IS NOT NULL;

DELETE FROM agent_context_summaries summary
WHERE NOT EXISTS (
    SELECT 1
    FROM agent_context_summary_rollups rollup
    WHERE rollup.canonical_id = summary.id
);

CREATE UNIQUE INDEX uq_agent_context_summaries_chat_id
    ON agent_context_summaries (chat_id);
