# Agent Memory System — Design Spec

**Date:** 2026-06-25  
**Status:** Approved

## Goal

Admin-grade agent memory: Postgres dialog history, hybrid context compacting, user facts CRUD, and a **Agents** UI tab (Memory | Traces) to inspect and control everything per Telegram `chat_id`.

## Decisions (brainstorming)

| Topic | Choice |
|-------|--------|
| UI audience | Admin: list/switch `chat_id` |
| Compacting | Hybrid: auto threshold + manual «Compact» + visible raw vs summary |
| Admin powers | Full: fact CRUD, exclude message, delete, reset compaction, clear dialog |
| UI structure | Single **Agents** route with subtabs **Memory** \| **Traces** |

## Current state (baseline)

- `agent_conversation_messages` — flat log per `chat_id`, JSON messages
- `agent_user_facts` — long-term facts, tool `manage_user_fact`
- LLM window: `agent.memory.recent-messages` (default 10)
- No compacting, no admin API, Traces via Grafana Tempo

## Architecture

### Data model (Flyway V7)

**New `agent_context_summaries`**

- `id` UUID PK
- `chat_id` BIGINT NOT NULL
- `sequence` INT NOT NULL — order in prompt
- `summary_text` TEXT NOT NULL
- `covers_message_id_from` / `covers_message_id_to` BIGINT — FK to `agent_conversation_messages.id`
- `source_message_count` INT
- `model` VARCHAR, `created_at` TIMESTAMPTZ
- optional `tokens_before` / `tokens_after` INT

**Extend `agent_conversation_messages`**

- `excluded_from_context` BOOLEAN DEFAULT false
- `compacted_into_summary_id` UUID NULL → `agent_context_summaries.id`

**`agent_user_facts`** — unchanged in v1 (optional `category` / `source` deferred).

### LLM context assembly

1. System: prompt + workout snapshot + facts (unchanged)
2. One rolling summary from `agent_context_summaries`
3. Recent raw messages: not excluded, `compacted_into_summary_id IS NULL`, last K = `agent.memory.recent-messages`
4. New user message

### Compacting

**Properties**

- `agent.memory.compact-threshold-messages` (default 40)
- `agent.memory.compact-model` (optional, default agent model)

**Auto:** after message append, if compactable raw count > threshold → async compact job.
Compactions for the same chat are serialized with a PostgreSQL advisory
transaction lock, so concurrent append hooks cannot create duplicate summaries.

**Manual:** `POST /api/admin/agent-memory/chats/{chatId}/compact`

**Algorithm**

1. Select oldest compactable raw messages; keep tail K untouched
2. LLM summarizes (structured: topics, decisions, numbers; do not duplicate facts table)
3. Create the chat summary on the first run; on later runs merge the existing
   summary with the new raw messages and update that same row
4. Set `compacted_into_summary_id` on covered messages

There is a database uniqueness constraint on `agent_context_summaries.chat_id`.
Migration V18 rolls older active summary blocks into one row per chat and removes
orphan rows that are not referenced by any message.

**Reset/delete summary:** delete the summary, clear
`compacted_into_summary_id`, and set covered messages back to
`is_compacted = false` so they can be compacted again.

### Admin API

Prefix `/api/admin/agent-memory/**`, JWT authenticated.

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/chats` | List chats + stats |
| GET | `/chats/{chatId}` | Facts + summaries + context preview |
| GET | `/chats/{chatId}/messages` | Paginated full history |
| POST | `/chats/{chatId}/facts` | Create fact |
| PUT | `/facts/{id}` | Update fact |
| DELETE | `/facts/{id}` | Delete fact |
| PATCH | `/messages/{id}` | `excluded_from_context` |
| DELETE | `/messages/{id}` | Delete message |
| POST | `/chats/{chatId}/compact` | Manual compact |
| POST | `/chats/{chatId}/reset-compaction` | Reset summaries |
| DELETE | `/chats/{chatId}/dialog` | Clear messages + summaries |

### Frontend (`my-utils`)

- **AgentsPage** — tabs: Memory | Traces
- **AgentMemoryPage** — chat list, facts editor, context panel (summaries + recent), full history timeline, action buttons
- **AgentTracesPage** — Grafana embed (fixed: no kiosk, all-traces panel, 7d range)
- Endpoints in `src/api/endpoints.ts`

### Traces fix (shipped separately)

- `GenAiTracing` → Spring `OpenTelemetry` bean (not static `GlobalOpenTelemetry`)
- Dashboard: «All agent traces» panel without empty `conversation_id` filter
- Embed: remove kiosk, `from=now-7d`

## Testing

- Unit: compact selection, message assembly, delta append
- API integration: admin CRUD + compact with stub LLM
- UI: smoke on Memory tab with mock or local API

## Out of scope (v1)

- Fact categories, vector RAG, multi-user identity beyond `chat_id`, automatic fact extraction on compact
