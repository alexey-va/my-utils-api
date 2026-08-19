# Go Backend Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to execute this plan task-by-task.

**Goal:** Replace the Kotlin/Spring production backend with a contract-compatible Go service that reuses the current PostgreSQL/Redis state, starts fresh Temporal workflows, and warms runtime settings plus JWT/session paths before serving traffic.

**Architecture:** One explicit Go process owns HTTP, persistence, Telegram polling and a Temporal worker. Existing SQL migrations and Redis key formats remain authoritative. A mandatory startup barrier completes migrations, runtime-setting cache load, JWT sign/verify, Redis probing and optional Temporal connection before readiness/listen.

**Tech Stack:** Go, `chi`, `pgx`, `go-redis`, `golang-jwt/jwt`, `x/crypto/bcrypt`, Temporal Go SDK, Prometheus client, PostgreSQL 16, Redis 7, Docker/Woodpecker.

**Spec:** `docs/superpowers/specs/2026-08-19-go-backend-migration-design.md`

## Global constraints

- Work test-first: add one failing behavior test, observe the expected failure, implement the smallest vertical slice, rerun it, then refactor.
- Do not edit applied SQL migrations or live runtime-setting values.
- Preserve existing environment names, REST paths, JSON, Redis keys, security decisions, Docker labels and public ports.
- Use `myutils-go-v1` and new workflow IDs; never touch old Temporal histories.
- Keep the original checkout available as a clean Kotlin behavior oracle and reconcile parallel changes before final cutover.
- Do not call real Telegram or OpenRouter endpoints from tests.

## Task 1: Establish the Go module and startup contract

**Files:** `go.mod`, `go.sum`, `cmd/my-utils-api/main.go`, `internal/config/*`, `internal/startup/*`, `internal/httpapi/health.go`, matching `*_test.go` files.

1. Write failing table tests for environment parsing, required production secrets, listener gating, and health/readiness output.
2. Add the module, config types, structured lifecycle and minimal health router.
3. Add a startup coordinator whose dependencies expose `Warm(context.Context) error`; listener creation happens only after all warmers pass.
4. Verify unit tests and race tests.

## Task 2: Preserve schema and persistence

**Files:** `internal/migrate/*`, `internal/store/*`, `src/main/resources/db/migration/*.sql`, integration tests.

1. Write failing integration tests that run all existing migrations on empty PostgreSQL and validate critical tables, indexes, constraints and Flyway history compatibility.
2. Implement ordered migration discovery/execution with checksums and PostgreSQL advisory locking, recognizing the existing `flyway_schema_history` rows.
3. Implement `pgxpool` stores and transaction helpers for users, workouts, health, settings, agent memory/test state and WireGuard.
4. Port repository behavior tests, especially sort/order, upsert, pagination, pessimistic sandbox updates and metric ranges.

## Task 3: Runtime settings and mandatory warmup

**Files:** `internal/settings/*`, `internal/startup/*`, tests.

1. Write failing tests for default seeding, validation fallback, atomic snapshot replacement, typed update, one-minute refresh, and update hooks.
2. Implement the complete property catalog from Kotlin with existing keys/defaults/editors/tags.
3. Load and validate all settings synchronously before readiness.
4. Add elapsed-time logging and readiness failure propagation.

## Task 4: Authentication and security policy

**Files:** `internal/auth/*`, `internal/httpapi/middleware/*`, `internal/httpapi/auth.go`, tests.

1. Write failing compatibility tests for HS JWT claims, expiry, invalid signature, Redis session ownership, revoke/logout, credential rotation, bootstrap admin, bcrypt, roles and default deny.
2. Implement JWT issue/parse and startup sign/verify warmup using subject `startup-warmup`.
3. Implement the isolated Redis create/read/delete startup probe.
4. Implement route policy and auth endpoints with current status/error behavior.

## Task 5: Health and workout REST vertical slices

**Files:** `internal/health/*`, `internal/workout/*`, `internal/httpapi/health_data.go`, `internal/httpapi/workouts.go`, tests.

1. Port controller/service tests to Go contract tests.
2. Implement fixed local-workout user semantics, exercises, entries, grid, progress, move and delete.
3. Implement steps positional import and body-weight sparse import/history.
4. Run tests against migrated PostgreSQL and compare representative JSON fixtures with Kotlin output.

## Task 6: Admin settings and agent memory/test console

**Files:** `internal/httpapi/admin_*`, `internal/agent/memory/*`, `internal/agent/sandbox/*`, tests.

1. Port settings and agent-memory controller tests.
2. Implement typed settings list/update with atomic cache updates and hooks.
3. Implement facts, message history, exclusion, deletion and compaction.
4. Implement test chats and pessimistically updated sandbox state.
5. Prove every sandbox tool is isolated and Telegram/status/chart/delivery actions simulate locally.

## Task 7: WireGuard control plane

**Files:** `internal/wireguard/*`, `internal/httpapi/wireguard.go`, tests.

1. Port relay, peer, credential, desired-state, heartbeat and metrics tests.
2. Implement token hashing/auth, AES-GCM-compatible credential encryption and address allocation.
3. Preserve no-store headers and host-agent role isolation.
4. Validate current ops scripts against generated desired/credential payload fixtures.

## Task 8: Telegram, OpenRouter agent and tools

**Files:** `internal/telegram/*`, `internal/openrouter/*`, `internal/agent/*`, tests.

1. Port Telegram access, long-polling/coalescing/status/file-upload tests with fake HTTP servers.
2. Implement OpenRouter chat/tool-call payloads, proxy, timeouts, normalization and retries.
3. Port the tool catalog, argument grounding, mutation policy, workout/health/fact/notification tools and memory compaction.
4. Preserve the exact stored sequence user → assistant tool call → tool result → assistant without duplicates.
5. Port weekly PNG reports and verify dimensions, readable tables, missing step placeholders and latest real weight rows.

## Task 9: Fresh Temporal workflows

**Files:** `internal/temporal/*`, tests with Temporal test environment.

1. Write failing deterministic tests for delayed notification, evening reminder, Saturday noon report and agent tool loop.
2. Implement workflows and activities on `myutils-go-v1` with new recurring workflow IDs.
3. Start the worker during the startup barrier, then ensure recurring workflows for allowed Telegram IDs.
4. Prove bootstrap never queries or terminates old Kotlin workflow IDs.

## Task 10: Build, deployment and cleanup

**Files:** `Dockerfile`, `.woodpecker.yml`, compose files, `Makefile`, docs, removal of Kotlin/Gradle sources after parity.

1. Switch CI verify to `go test -race ./...`, integration tests and `go vet ./...`.
2. Build a multi-stage static image, run as non-root and keep current port, health check, labels and fonts needed for charts.
3. Update compose while retaining PostgreSQL/Redis/Temporal topology and production environment names.
4. Reconcile any newer `main` changes, translate their behavior, and rerun all checks.
5. Remove Kotlin/Gradle implementation only when every replacement slice is green; retain immutable SQL migrations and useful historical docs.
6. Run `git diff --check`, full tests, image build and local compose smoke; record startup warmup logs, REST smoke and memory.
7. Commit, push to `main`, wait for Woodpecker, then production read back health, auth/session, key REST surfaces, Temporal worker/recurring workflows, logs, RSS and swap.
