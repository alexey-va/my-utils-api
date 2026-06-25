# Agent Memory System Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admin UI + API for agent dialog history, hybrid compacting, and user facts; Agents tab with Memory | Traces subtabs.

**Architecture:** Summary table + extended messages in Postgres; `AgentMemoryService` for assembly/compact; REST under `/api/admin/agent-memory`; React Memory subtab.

**Tech Stack:** Kotlin Spring Boot 3.4, Flyway, JPA, React 19, Ant Design, Refine, Linear tokens.

**Spec:** `docs/superpowers/specs/2026-06-25-agent-memory-design.md`

---

## File map

| Area | Files |
|------|-------|
| Migration | `src/main/resources/db/migration/V7__agent_memory_compacting.sql` |
| Domain | `AgentContextSummary.kt`, repositories |
| Memory | `agent/memory/AgentMemoryAssembler.kt`, `AgentContextCompactionService.kt`, extend `AgentConversationStore` |
| API | `web/AdminAgentMemoryController.kt`, `dto/AgentMemoryDtos.kt` |
| Properties | `Properties.kt` — compact threshold + model |
| Frontend | `AgentsPage.tsx`, `AgentMemoryPage.tsx`, move traces, `api/agentMemory.ts`, `endpoints.ts`, `featureCatalog` label |
| Tests | compaction unit, API controller test, assembler test |

---

### Task 1: Flyway V7 schema

**Files:** Create `V7__agent_memory_compacting.sql`

- [ ] Create `agent_context_summaries` table + indexes on `chat_id`, `(chat_id, sequence)`
- [ ] Add `excluded_from_context`, `compacted_into_summary_id` to `agent_conversation_messages`
- [ ] Run `./gradlew flywayMigrate` locally (docker postgres)

---

### Task 2: Domain entities + repositories

**Files:** `domain/AgentContextSummary.kt`, `AgentContextSummaryRepository.kt`, update `AgentConversationMessage.kt`

- [ ] JPA entities matching migration
- [ ] Repository: `findByChatIdOrderBySequence`, `findCompactableMessages`, chat list aggregation query

---

### Task 3: Context assembly

**Files:** `agent/memory/AgentMemoryAssembler.kt`, modify `WorkoutLangChain4jAgent.buildLlmMessages`

- [ ] Load summaries + recent raw (not excluded, not compacted)
- [ ] Replace `conversationStore.loadRecent` in LLM path with assembler
- [ ] Keep LangChain4j direct path consistent

---

### Task 4: Compaction service

**Files:** `agent/memory/AgentContextCompactionService.kt`, `Properties.kt`

- [ ] Properties: `agent.memory.compact-threshold-messages`, `agent.memory.compact-model`
- [ ] `compact(chatId, force)` — LLM summarize + persist summary + mark messages
- [ ] `resetCompaction(chatId)`, hook after `AgentConversationStore.persist`
- [ ] Unit tests: selection logic, no compact of tail K

---

### Task 5: Admin REST API

**Files:** `web/AdminAgentMemoryController.kt`, `dto/AgentMemoryDtos.kt`, `SecurityConfig` (already `/api/admin/**` auth)

- [ ] Implement endpoints from spec
- [ ] Delegate facts to `AgentUserFactsService`
- [ ] `AdminAgentMemoryControllerTest` with `@MyUtilsSpringTest`

---

### Task 6: Frontend Agents tabs + Memory UI

**Files:** `features/agents/AgentsPage.tsx`, `AgentMemoryPage.tsx`, refactor `AgentTracesPage.tsx`

- [ ] `AgentsPage` with Ant `Tabs`: Memory | Traces
- [ ] Chat list sidebar, facts CRUD, context panel, history timeline
- [ ] API client `src/api/agentMemory.ts`
- [ ] Update `features.tsx` — route `/agents` → `AgentsPage`
- [ ] Label catalog: «Agents» (not «Agent traces»)

---

### Task 7: Deploy + verify

- [ ] `./gradlew test` (docker postgres/redis)
- [ ] Jenkins API + MyUtils frontend
- [ ] Manual: compact, exclude message, traces tab shows `invoke_agent` after bot message

---

## Traces fix (done in parallel)

- [x] `GenAiTracing` → `@Component` + Spring `OpenTelemetry`
- [x] Grafana dashboard v2: all-traces panel, 7d default
- [x] Remove kiosk from traces embed; time range in URL
