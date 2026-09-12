# my-utils-api — architecture

## Stack and process

Go 1.26 · chi · pgx/PostgreSQL 16 · Redis 7 · Temporal Go SDK · OpenRouter ·
Telegram Bot API. Production runs one statically linked binary in Alpine.

`cmd/my-utils-api/main.go` wires the process. The listener starts only after
migrations, bootstrap admin, settings cache, JWT/Redis probes, Temporal worker
and Telegram runner are ready. SIGTERM cancels workers and drains HTTP.

## Data

- PostgreSQL: users, exercises, workout entries, health data, `app_settings`,
  agent messages/facts/summaries/test sandboxes and WireGuard desired state.
- Redis: JWT access-session records plus hashed, sliding refresh sessions and
  per-user revocation indexes.
- Agent messages keep OpenAI-compatible assistant tool calls and tool results as
  JSON. One rolling summary covers compacted history; a verbatim tail remains.
- SQL migrations stay under `src/main/resources/db/migration/`. The Go runner
  preserves Flyway version/description/type/checksum semantics for the existing
  production database.

## HTTP security

| Path | Access |
| --- | --- |
| `/api/health`, `/actuator/health`, `/actuator/prometheus` | public |
| `/api/auth/login`, `/api/auth/register`, `/api/auth/refresh` | public; refresh uses an HttpOnly SameSite cookie |
| `/api/workouts/**`, steps and body-weight endpoints | public personal-instance data |
| `POST /api/client-events` | public, sanitized telemetry |
| `POST /api/telegram/files` | public route plus constant-time upload-token check |
| remaining `/api/auth/**` | JWT plus live Redis session |
| `/api/admin/**` | ready `ADMIN` only |
| `/api/internal/wireguard/**` | relay-scoped agent token |
| unknown route | anonymous `401`; authenticated `404`/`405` |

The frontend tab password is only a visibility gate. The route registration and
middleware in `internal/httpapi/` are the server-side source of truth.

Access JWTs keep the existing 24-hour JJWT-compatible contract. Login creates a
separate opaque refresh credential whose SHA-256 digest is stored in Redis. The
cookie is HttpOnly, SameSite=Lax, scoped to `/api/auth`, and Secure in
production. Successful refresh extends its 30-day idle TTL; logout and
credential changes revoke the corresponding Redis records.

## Telegram and agent

```text
Telegram getUpdates
  → serial per-chat runner
  → allowlist check
  → voice file download + OpenRouter STT (voice messages only)
  → durable per-chat Temporal FIFO (when enabled) or locked direct turner
  → structured resolve_turn decision (read / write / clarify)
  → whole-plan schema, intent evidence and numeric validation
  → exact tool execution and persisted receipts
  → stored final assistant message
  → Telegram reply
```

`internal/agent/turner.go` owns interpretation, validation, execution and final
receipts. The model produces a typed `resolve_turn` decision. A read decision
cannot escalate to writes in later tool steps. Every write carries a literal
current-request quote; workout numbers must also match a single user's data
quote. Numeric validation never consumes, reassigns or overwrites data. The
interpreter resolves contextual intent (including corrections and short replies),
so no lexical allowlist decides whether a natural sentence is writable.

The entire plan is validated before its first side effect. Duplicate operations
and delete-then-write replacements are rejected. All database actions and their receipts commit in one transaction; a failed
action rolls back the plan. External deliveries cannot join a database plan.
Mutation results become the
stored final reply directly; no second model completion can invent success or
unsolicited training advice. Each attempted tool call retains exact arguments,
matching call ID and an `ok/result/error` receipt. A failed operation is not
blindly replayed. `copy_workout` reads exact sets from the database: by default the latest
session before the target date, or an explicitly selected `source_date`. An
explicit weight override keeps the repetitions. A date move copies the selected
source before deleting it in the same transaction. Existing exercises resolve
by canonical ID; exact names take priority over fuzzy matches.

`agent.memory.recent-messages` now counts complete user turns, including every
tool round and final reply; the current turn remains in memory throughout
execution. Compaction keeps whole turns and excludes the newest one. Exact older
attempts remain available through same-chat `get_conversation_history`, including
compacted failures. Summary is background, never evidence of a mutation.

`AgentTurnQueueWorkflow` receives durable signals using a stable per-chat ID,
executes one activity at a time without automatic mutation retries, and carries
pending signals across ContinueAsNew. Legacy one-shot workflows remain registered.
The direct/test path uses the same turner with an in-process per-chat lock.

OpenRouter action names are snake_case end to end. The test console uses a
persisted sandbox; unsupported operations fail closed instead of reaching real
workout, health, facts, notifications or Telegram delivery.

The fresh workout/health snapshot is rebuilt for each LLM step. Historic
messages therefore do not own current-day or current-week state. Auto
compaction is queued outside the request path, respects tool-call boundaries,
and keeps one rolling summary.

The optional WireGuard self-service bot has a different bot token, client,
runner and dispatcher in the same process. Its private-chat callback flow is
fully deterministic and never reaches the agent or OpenRouter. PostgreSQL owns
the `PENDING`/`APPROVED`/`REJECTED`/`BLOCKED` access state, tunnel limit,
peer-to-Telegram-user ownership and audit events. Every credential or peer
mutation resolves ownership server-side; blocking an account changes its state
and every owned peer in one PostgreSQL transaction. Database mutations commit
with their audit event, while Telegram credential delivery persists an attempt
before sending and records the outcome afterward. QR codes and `.conf` files
are generated locally and sent with Telegram content protection.

## Temporal

Task queue: `myutils-go-v1`.

| Workflow | ID pattern | Purpose |
| --- | --- | --- |
| Agent turn | `go-v1-agent-turn-{chatId}-{uuid}` | durable LLM/tool turn |
| Evening reminder | `go-v1-evening-reminder-{chatId}` | daily workout nudge |
| Weekly health report | `go-v1-weekly-health-report-{chatId}` | Saturday PNG reports |
| Notification | `go-v1-tg-notify-{chatId}-{uuid}` | delayed Telegram message |

The Go bootstrap starts only these IDs and never inspects or mutates histories
from the removed Kotlin task queue. `time/tzdata` is embedded so workflow time
zones work in the small Alpine image.

## Runtime settings

`internal/settings/catalog.go` defines typed values backed by `app_settings`.
They refresh every minute. Agent and voice-transcription models, OpenRouter
retry policy, agent context and compaction, report/reminder timing are read
through callbacks so edits apply without rebuilding clients. Existing values
must be changed through the admin API and read back.

## Observability

- JSON logs to stdout → Promtail/Loki (`app=my-utils-api`).
- `/actuator/prometheus` exposes Go runtime, process RSS, HTTP, agent, LLM and
  tool metrics.
- Grafana source lives in `observability/`; dashboard UID remains stable.
- `POST /api/client-events` strips query strings/control characters and ignores
  unknown or invalid fields before structured logging.

## Development and deployment

```bash
docker compose -f docker-compose.dev.yml up -d
go run ./cmd/my-utils-api
TEST_POSTGRES_URL='postgres://myutils:myutils@localhost:5432/myutils?sslmode=disable' go test ./...
go vet ./...
```

Push to `main` starts Woodpecker and the server-side deploy script. The
production overlay binds API/Temporal only on localhost; nginx owns public
routing. The `docker-compose.jenkins.yml` name is historical.

## Optional model behavior checks

`cmd/agent-eval` is an opt-in synthetic dialogue runner. It creates disposable
sandbox chats, has no real workout/health/Telegram adapters, reads state after
each turn and removes only the chats it created. Ordinary `go test` uses fakes
and never calls OpenRouter. Run it only with an explicitly selected local test
database and approved model credentials; do not import production conversations
into a model eval without the user's specific authorization.

```bash
AGENT_EVAL_DATABASE_URL=... AGENT_EVAL_BASE_URL=... AGENT_EVAL_API_KEY=... \
AGENT_EVAL_MODEL=... go run ./cmd/agent-eval cmd/agent-eval/testdata/scenarios.json
```

The checked-in synthetic corpus covers decimals and typos, historical copying,
short clarification/correction, independent multi-exercise values and pounds,
hypothetical instructions, exact failed-call history, ambiguous exercise IDs,
copy overrides/date moves, and failed multi-action plans. Inspect each result's
`turns` (including linked calls and receipts) and `states` against the requested
changes; a fluent reply alone is not a pass.
