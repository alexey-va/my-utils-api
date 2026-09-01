# AGENTS.md — my-utils-api

Go 1.26 API with PostgreSQL, Redis, Temporal Go SDK, OpenRouter and Telegram.
Docker-first; the production runtime is one static binary on Alpine.

This is an independent Git repository. Frontend changes belong in
`../my-utils` and must be committed and verified separately.

## Layout

```text
cmd/my-utils-api/       process wiring, startup barrier, healthcheck command
internal/httpapi/       chi routes, DTO validation and access policy
internal/auth/          JWT and Redis sessions
internal/workout/       workout diary
internal/health/        steps and body weight
internal/wireguard/     relay desired state and credential encryption
internal/agent/         memory, tool loop, sandbox and compaction
internal/openrouter/    OpenAI-compatible HTTP client
internal/telegram/      Bot API, polling and file delivery
internal/temporal/      workflows, activities and worker service
internal/report/        pure-Go PNG renderers
internal/settings/      typed runtime properties backed by app_settings
internal/observability/ Prometheus instrumentation
src/main/resources/db/migration/ immutable SQL migrations plus embed.go
observability/          versioned Grafana/Loki/Promtail source
```

## Commands

| Task | Command |
| --- | --- |
| Full stack | `docker compose up -d --build` |
| Rebuild API | `docker compose up -d --build api` |
| Infra only | `docker compose -f docker-compose.dev.yml up -d` |
| Host API | `go run ./cmd/my-utils-api` |
| Tests | `TEST_POSTGRES_URL=... go test ./...` |
| Static checks | `go vet ./...` |

Tests requiring PostgreSQL skip without `TEST_POSTGRES_URL`. Start migrations
first when packages share one fresh database; CI already does this. Redis is
needed by runtime startup and auth integration smoke, not by every unit test.

## Invariants

- Keep all public REST paths and payload shapes compatible with
  `../my-utils/src/api/endpoints.ts` and its API types.
- Existing runtime values change only through authenticated
  `PUT /api/admin/settings/{key}` plus read-back. Flyway SQL is only for new
  settings/schema, and applied migration files are immutable.
- Startup is fail-fast. Do not move HTTP listen before migrations, settings,
  JWT, Redis, Temporal and Telegram warmers.
- JWT sessions remain JJWT-compatible HMAC tokens plus Redis session IDs.
- Workout and health routes remain public for this personal instance; admin
  routes require a ready `ADMIN`; WireGuard internal routes require agent token.
- Unknown routes stay default-deny.
- Test-console tools are sandbox-only and must never fall through to real data.
- Temporal task queue is `myutils-go-v1`; never reuse or mutate Kotlin workflow
  histories.
- Preserve complete assistant tool-call + tool-result boundaries when selecting
  messages for compaction.
- Versioned observability source is `observability/`, not the sibling local copy.

## Agent tool changes

1. Add or update the JSON schema in `internal/agent/tools_catalog.go`.
2. Implement one branch in `internal/agent/tools.go`.
3. Ground mutations in the current user text and keep the mutation policy.
4. Implement the corresponding sandbox behavior; fail closed if unsupported.
5. Test the stored `user → assistant(tool_call) → tool → assistant` sequence.

## Verification and deployment

Minimum gate:

```bash
TEST_POSTGRES_URL='postgres://myutils:myutils@localhost:5432/myutils?sslmode=disable' go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -buildvcs=false ./cmd/my-utils-api
git diff --check
```

Pushes and pull requests run the heavy verification gate in GitHub Actions.
For a push to `main`, Woodpecker waits for that exact commit's successful
GitHub Actions run and then performs only the production Docker deployment.
A push is not a test; do not push when the local gate is red. Secret changes,
manual restarts and external infrastructure changes need separate authorization.
