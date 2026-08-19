# Go Backend Migration Design

**Date:** 2026-08-19

## Goal

Replace the Kotlin/Spring/JVM application with one Go process while preserving the public REST contract, the existing PostgreSQL data, Redis JWT sessions, Telegram agent behavior, WireGuard control plane, observability labels, and scheduled automations. The production result must use materially less resident memory and must not accept traffic before its runtime caches and JWT implementation are warmed.

## Scope and constraints

- The Go service becomes the only production `api` container. There is no dual-write or shadow production service.
- PostgreSQL tables and data remain authoritative. Existing Flyway SQL migrations are immutable and continue to be applied in their current order by the Go binary.
- Existing Redis session keys remain valid: `myutils:session:<jti>` contains the user UUID and `myutils:user-sessions:<user-id>` contains session IDs.
- Public paths, request and response JSON, status codes, multipart behavior, security policy, `Cache-Control` headers, and frontend assumptions stay compatible.
- Existing Temporal histories are deliberately not migrated. The Go worker and clients use the fresh task queue `myutils-go-v1`. Old task queues and histories may be left for Temporal retention.
- Existing `app_settings` values may only be mutated through the typed admin API. The migration must not rewrite live values through SQL or defaults.
- The admin agent test console remains fail-closed and sandbox-only. It never reads or mutates the real workout diary and never sends Telegram messages.
- Production secrets remain environment-only.

## Cutover model

The deployment is a clean binary replacement:

1. CI runs Go unit and integration tests against PostgreSQL and Redis.
2. CI builds a static Linux binary and a small runtime image.
3. Compose replaces only the `api` container. PostgreSQL, Redis, Temporal server, Temporal database, and Temporal UI remain in place.
4. The new service applies any pending existing SQL migrations under a PostgreSQL advisory lock.
5. It completes startup warmup, registers a Go Temporal worker on `myutils-go-v1`, and creates new recurring workflows for allowed Telegram users.
6. Only after all mandatory startup gates pass does it open port 8080 and report ready.

Rollback is the previous JVM image against the unchanged schema and Redis key layout. New Go workflow histories are isolated by task queue, so rollback does not make the JVM worker consume them.

## Go architecture

Use a small explicit dependency graph rather than a framework container:

- `cmd/my-utils-api`: configuration, lifecycle, migrations, startup warmup, HTTP and Temporal worker startup, graceful shutdown.
- `internal/config`: environment parsing and validation with the current environment variable names and defaults.
- `internal/store`: `pgxpool` repositories and transactions over the existing schema.
- `internal/cache`: immutable runtime-setting snapshot behind atomic replacement plus periodic refresh.
- `internal/auth`: bcrypt, HS JWT, Redis sessions, HTTP authentication/authorization middleware.
- `internal/httpapi`: `chi` routes, DTOs, validation, error mapping, CORS, multipart limits, health and Prometheus endpoints.
- `internal/workout`, `internal/health`, `internal/wireguard`: domain services preserving current behavior.
- `internal/agent`: OpenRouter tool loop, memory/compaction, tool policy, sandbox console, Telegram delivery.
- `internal/temporal`: Go workflows and activities using the fresh task queue.
- `internal/telegram`: long polling, inbound coalescing, status edits, files and photos.
- `internal/observability`: structured JSON logging and Prometheus metrics with current names/labels where consumed.

Dependencies should remain narrow: standard library, `chi`, `pgx`, `go-redis`, `golang-jwt/jwt`, `x/crypto/bcrypt`, Temporal Go SDK, Prometheus client, and a maintained Go chart renderer. External Telegram and OpenRouter calls use explicit HTTP clients so timeouts, proxy behavior, payloads, and test fakes are visible.

## Mandatory startup barrier

Startup is fail-closed and ordered:

1. Parse and validate configuration without logging secret values.
2. Connect to PostgreSQL and Redis and require successful pings.
3. Apply the unchanged SQL migrations and validate the expected schema.
4. Seed only missing `app_settings` definitions and synchronize their metadata, matching current behavior.
5. Load every known runtime property into one validated in-memory snapshot. Invalid stored values fall back to the code default and are logged without exposing secrets. Refresh the snapshot every minute by atomic swap.
6. Warm JWT end to end: derive the HMAC key, sign a deliberately expired token with subject `startup-warmup`, parse and verify its signature while allowing the expected expiry result. A broken key or algorithm fails startup.
7. Exercise the Redis session path with an isolated random key inside a transaction-like create/read/delete probe so the request path is initialized without touching real sessions.
8. If Temporal is enabled, connect the client and start the worker before recurring workflows are ensured.
9. If Telegram is enabled, validate the non-secret structural configuration; external polling begins only after the API lifecycle starts.
10. Mark readiness true and open the HTTP listener.

No request can race a lazy JWT key derivation or a cold/missing runtime-settings cache. Startup logs include elapsed milliseconds for migrations, runtime settings, JWT, Redis probe, Temporal, and total startup, but never include secret material or tokens.

## Contract preservation

The current Kotlin controllers and tests are the behavior oracle during the migration. Go contract tests cover:

- route, method, content type, payload field names, nullability, ordering, and status codes;
- permit-all, authenticated, admin, WireGuard-agent and default-deny branches;
- JWT claim names and Redis session ownership/revocation;
- health steps positional import and sparse body-weight history;
- workout grid, progress, move and delete semantics;
- typed runtime-setting serialization, validation, update hooks and refresh;
- Telegram file token derivation, 20 MB limit, captions and recipient fan-out;
- WireGuard token hashes, desired state, credentials encryption, no-store responses, heartbeat counters and metric samples;
- agent memory compaction and exact tool-call/tool-result ordering;
- sandbox isolation and simulated delivery;
- Temporal reminder, notification, agent-turn and Saturday report timing.

The frontend repository should require no change. Any discovered contract mismatch is fixed in Go unless it exposes a proven bug that the user explicitly chooses to change.

## Temporal reset

All Go workflow starts use task queue `myutils-go-v1`. Workflow IDs receive a Go generation marker for recurring workflows (`go-v1-evening-reminder-<chat>`, `go-v1-weekly-health-report-<chat>`); one-shot notification and agent workflows keep random UUID suffixes. The bootstrap does not terminate, signal, query, or reuse Kotlin workflow IDs. This is intentional because the old queue is empty and its history is disposable.

Long-running workflow code remains deterministic. Database, HTTP, rendering, LLM and Telegram work stays in activities. Activity timeouts and retry limits match the Kotlin behavior unless an integration test demonstrates a stricter current contract.

## Observability and memory acceptance

- `/api/health`, `/actuator/health`, and `/actuator/prometheus` remain available at their current paths.
- App logs remain JSON on stdout with Docker labels `logging=promtail` and `app=my-utils-api`.
- Existing application metrics used by dashboards keep compatible names; Go runtime/process metrics replace JVM-specific metrics.
- The Docker health check begins only after the startup barrier can succeed.
- Production acceptance records container RSS, swap and image size after warm traffic. The migration target is at least 250 MiB lower steady-state RSS than the JVM baseline; functionality and data correctness remain hard gates even if the target is missed.

## Removal boundary

Kotlin sources, Gradle files and JVM Docker stages are removed only after the corresponding Go vertical slices pass. Historical design documents and immutable SQL migrations stay. `AGENTS.md`, README and architecture documentation are updated to describe Go commands and layout at cutover.
