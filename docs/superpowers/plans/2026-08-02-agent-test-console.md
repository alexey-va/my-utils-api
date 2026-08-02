# Workout Agent Test Console Implementation Plan

> **For agentic workers:** execute each task with strict red-green-refactor
> cycles. Do not push until both repositories pass their complete local gates.

**Goal:** Add first-class admin test chats that run the real Workout assistant,
persist complete tool rounds, and expose a production-ready chat console.

**Architecture:** A new `agent_test_chats` table maps an API UUID to an isolated
memory `chatId` and a real user/tool context `chatId`. Optional context IDs flow
through the existing direct and Temporal agent paths without changing existing
Telegram behavior. The React console reuses the current stored-message and tool
round renderers.

**Tech stack:** Kotlin, Spring Boot, JPA, Flyway, Temporal, LangChain4j,
PostgreSQL, React 19, TypeScript, Ant Design, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-02-agent-test-console-design.md`

---

## Task 1: Prove and implement conversation/context identity separation

**Backend files**

- Modify `src/main/kotlin/dev/myutils/api/temporal/agent/AgentTurnInput.kt`
- Modify `src/main/kotlin/dev/myutils/api/temporal/agent/AgentDtos.kt`
- Modify `src/main/kotlin/dev/myutils/api/temporal/agent/WorkoutAgentWorkflowImpl.kt`
- Modify `src/main/kotlin/dev/myutils/api/agent/langchain/WorkoutLangChain4jAgent.kt`
- Modify `src/main/kotlin/dev/myutils/api/agent/memory/AgentChatTurnService.kt`
- Modify `src/test/kotlin/dev/myutils/api/temporal/TemporalWorkflowTests.kt`
- Modify/add focused direct-agent tests

- [ ] Write a failing Temporal test: LLM memory uses the test conversation ID,
      tool execution receives the real context ID, and results return to the
      test conversation.
- [ ] Write a failing direct-agent test proving user facts/system context use
      the context ID while messages persist under the memory ID.
- [ ] Add optional `contextChatId`, defaulting to `chatId`, through the workflow
      and direct path.
- [ ] Run focused tests and refactor only after green.

## Task 2: Add test-chat persistence and lifecycle service

**Backend files**

- Create `src/main/resources/db/migration/V21__agent_test_chats.sql`
- Create `src/main/kotlin/dev/myutils/api/domain/AgentTestChat.kt`
- Create `src/main/kotlin/dev/myutils/api/domain/AgentTestChatRepository.kt`
- Create `src/main/kotlin/dev/myutils/api/agent/memory/AgentTestChatService.kt`
- Create `src/test/kotlin/dev/myutils/api/agent/memory/AgentTestChatServiceIntegrationTest.kt`

- [ ] Write failing integration tests for empty-chat creation, reserved and
      unique memory IDs, newest-activity ordering, rename validation, and `404`.
- [ ] Add the Flyway table/sequence and JPA mapping.
- [ ] Implement create/list/get/rename and activity timestamps.
- [ ] Write a failing deletion test proving messages/summaries are removed but
      real-context facts and Workout data are untouched.
- [ ] Implement transactional clear/delete behavior.

## Task 3: Add the admin HTTP contract and real send endpoint

**Backend files**

- Create `src/main/kotlin/dev/myutils/api/web/AdminAgentTestChatController.kt`
- Create `src/main/kotlin/dev/myutils/api/web/dto/AgentTestChatDtos.kt`
- Modify `src/main/kotlin/dev/myutils/api/agent/memory/AgentTestChatService.kt`
- Add `src/test/kotlin/dev/myutils/api/web/AdminAgentTestChatControllerIntegrationTest.kt`
- Update `docs/ARCHITECTURE.md`, `README.md`, and `../README.md`

- [ ] Write failing API tests for create/list/detail/rename/messages/clear/delete
      and admin authorization.
- [ ] Implement DTO validation and controller routes.
- [ ] Write a failing send test with the testing chat model that proves the
      user and assistant messages are persisted.
- [ ] Add a deterministic tool-call/result fixture at the external model
      boundary and prove the response returns the full stored round.
- [ ] Implement send by delegating to `AgentChatTurnService` with the two IDs.
- [ ] Run focused backend tests.

## Task 4: Add typed frontend API

**Frontend files**

- Create `src/api/agentTestChats.ts`
- Add `src/api/agentTestChats.test.ts`
- Modify `src/api/endpoints.ts` if the project endpoint catalog requires it

- [ ] Write failing request-contract tests with literal paths and payloads.
- [ ] Implement typed create/list/get/rename/delete/list-messages/send/clear
      calls using `apiClient`.
- [ ] Run the focused Vitest file.

## Task 5: Build the test console UI

**Frontend files**

- Create `src/features/agents/AgentTestConsolePage.tsx`
- Add focused component/state tests
- Modify `src/features/agents/AgentsPage.tsx`
- Reuse `AgentMemoryHistoryItem.tsx`, `agentMemoryFormat.ts`, and image helpers
- Modify `src/index.css`

- [ ] Write failing UI tests for create/select/send and the persistent
      `SANDBOX` isolation notice.
- [ ] Add Test console / Memory views, defaulting to Test console.
- [ ] Add chat sidebar, new/rename/delete controls, clear confirmation,
      chronological history, image composer, Enter-to-send, and loading/error
      states.
- [ ] Reuse the existing structured tool-round UI; do not duplicate parsing.
- [ ] Add desktop and narrow responsive styles using existing Linear tokens.
- [ ] Run focused frontend tests.

## Task 6: Complete local verification

- [ ] Backend: start isolated PostgreSQL and Redis test dependencies.
- [ ] Backend: run `./gradlew test` and `git diff --check`.
- [ ] Frontend: run `npm exec eslint -- src`, `npm test`, `npm run build`, and
      `git diff --check`.
- [ ] Review both diffs for unrelated or generated changes.
- [ ] Commit backend and frontend separately.

## Task 7: Delegated QA and production deployment

- [ ] Delegate the authenticated browser interaction/visual pass to
      `web_qa_tester` using the `delegate-quality-evals` workflow.
- [ ] Fix only confirmed failures and rerun the relevant local gates.
- [ ] Push backend `main`, wait for terminal Woodpecker success, and read back
      API health plus the new admin routes.
- [ ] Push frontend `main`, wait for terminal Woodpecker success.
- [ ] Run a production browser smoke: create a test chat, send a harmless
      read-only prompt that forces a real tool call, verify stored tool call and
      result, reload and verify persistence.
- [ ] Do not invoke outbound Telegram tools or mutating Workout tools during
      the deployment smoke.
- [ ] Capture and show the finished production console screenshot.
