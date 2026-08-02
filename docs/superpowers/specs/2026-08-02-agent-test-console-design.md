# Workout Agent Test Console — Design Spec

**Date:** 2026-08-02
**Status:** Approved from the goal: real LLM, real tools, and database writes

## Goal

Add an admin-only console for running separate test conversations with the
Workout assistant. A test turn must use the production agent path, persist the
complete conversation (including tool calls and tool results), execute real
tools, and expose the stored history in the SPA.

## Existing foundation

- `AgentChatTurnService` already runs a synchronous turn through Temporal when
  enabled and falls back to the direct LangChain4j loop otherwise.
- `agent_conversation_messages` already persists user, assistant, and tool
  messages.
- The Agents UI already knows how to render stored tool rounds.
- The current admin simulator is addressed by a raw Telegram `chat_id`; there
  is no first-class test-chat lifecycle, title, or isolated conversation
  identity.

## Product decisions

- Test conversations have their own memory and history.
- Workout, body-weight, and other domain tools operate on the real application
  database. This is signposted as `LIVE DATA` in the UI.
- Personal facts and Telegram-bound tools use the configured real user context,
  while test conversation messages remain isolated.
- A test turn never sends the assistant's final reply to Telegram.
  Telegram-bound tools may still deliver when the user explicitly asks the
  assistant to call them.
- Existing Telegram chats and the existing memory administration API remain
  unchanged.

## Backend design

### Data model

Add `agent_test_chats`:

- `id` UUID primary key, used by the public admin API;
- `memory_chat_id` BIGINT unique, allocated from a reserved negative range and
  used only as the key in existing memory tables;
- `user_context_chat_id` BIGINT, resolved from the configured allowed Telegram
  user and used for facts and tools;
- `title`, `created_at`, `updated_at`.

Deleting a test chat deletes its isolated messages and summaries, but never
deletes Workout data or personal facts written by tools.

### Conversation identity split

The agent turn carries two identities:

- `chatId`: conversation-memory identity;
- `contextChatId`: optional real user/tool identity, defaulting to `chatId` for
  all existing Telegram flows.

LLM history is loaded and recorded under `chatId`. The system prompt's personal
facts and tool execution use `contextChatId`. Tool results are recorded back
under `chatId`.

The new optional field keeps existing Temporal inputs and Telegram behavior
backward-compatible.

### Admin API

Prefix: `/api/admin/agent-test-chats`, protected by the existing `ROLE_ADMIN`
rule.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/admin/agent-test-chats` | Create a named empty test chat |
| `GET` | `/api/admin/agent-test-chats` | List test chats, newest activity first |
| `GET` | `/api/admin/agent-test-chats/{id}` | Get metadata and stats |
| `PATCH` | `/api/admin/agent-test-chats/{id}` | Rename |
| `DELETE` | `/api/admin/agent-test-chats/{id}` | Delete isolated chat history and metadata |
| `GET` | `/api/admin/agent-test-chats/{id}/messages` | Paginated stored history |
| `POST` | `/api/admin/agent-test-chats/{id}/messages` | Run one real agent turn |
| `DELETE` | `/api/admin/agent-test-chats/{id}/messages` | Clear isolated history |

The send response contains the final reply and every newly persisted message,
including assistant tool calls and tool results.

## Frontend design

The protected `/agents` page gains two views:

- **Test console** (default): test-chat list, create/rename/delete controls,
  history, image attachments, composer, and visible tool call/result cards.
- **Memory**: the existing low-level Telegram memory administration screen.

The console shows:

- a persistent `LIVE DATA` warning;
- a disabled/loading composer during a turn;
- chronological persisted messages;
- existing structured tool-round rendering;
- clear-history and delete-chat confirmations;
- responsive sidebar/main-column layout.

## Error handling

- Unknown test-chat IDs return `404`.
- Empty titles and empty message payloads return validation errors.
- Missing LLM/Temporal wiring preserves the existing `503` behavior.
- A failed turn remains inspectable: messages already persisted by the real
  pipeline are not silently rolled back.

## Verification

- Backend integration tests cover create/list/rename/delete, pagination,
  isolation, and one test turn with stored tool-call/result messages.
- Temporal and direct-path tests prove memory identity and tool/user context
  are split without changing normal Telegram turns.
- Frontend tests cover API contracts and core console state.
- Run the full backend suite, frontend lint/tests/build, focused delegated web
  QA, both Woodpecker pipelines, and production read-back.

## Out of scope

- A sandbox copy of Workout data.
- Multi-user ownership beyond the existing admin boundary.
- Streaming partial model output.
- Automatically executing outbound Telegram tools during deployment smoke
  tests.
