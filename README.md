# my-utils-api

Go REST API for [my-utils](https://github.com/alexey-va/my-utils).

**Для разработки:** [AGENTS.md](AGENTS.md) ·
**Документация:** [docs/README.md](docs/README.md) ·
**Архитектура:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Stack

- **Go 1.26** — один статический application binary;
- **PostgreSQL 16** — users, workout, health, settings, agent memory; SQL-миграции совместимы с существующей таблицей Flyway history;
- **Redis 7** — JWT sessions;
- **Temporal Go SDK** — agent turns, reminders, notifications and Saturday reports;
- **OpenRouter + Telegram Bot API** — прямые HTTP-клиенты без JVM/Chromium.

## Run everything in Docker

```bash
DOCKER_BUILDKIT=1 docker compose up -d --build
```

| Service | URL |
| --- | --- |
| API | http://localhost:8080 |
| Postgres | localhost:5432 (`myutils` / `myutils`) |
| Redis | localhost:6379 |
| Temporal | `localhost:7233` (gRPC) |
| Temporal UI | http://localhost:8233 |

```bash
docker compose up -d --build api  # rebuild API
docker compose logs -f api
docker compose down
```

Health: `GET http://localhost:8080/api/health`. Container healthcheck invokes
the application binary itself and does not install curl.

## Local development and tests

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up -d
go run ./cmd/my-utils-api
```

The complete local gate needs PostgreSQL and Redis:

```bash
TEST_POSTGRES_URL='postgres://myutils:myutils@localhost:5432/myutils?sslmode=disable' go test ./...
go vet ./...
CGO_ENABLED=0 go build ./cmd/my-utils-api
git diff --check
```

Integration tests never call real Telegram or OpenRouter endpoints. If Go is
not installed on the host, use the same `golang:1.26.5-alpine` image as CI.

## Startup contract

The HTTP listener opens only after all startup warmers succeed, in order:

1. SQL migrations;
2. bootstrap admin;
3. runtime settings cache;
4. JWT sign/verify warmup;
5. Redis session write/read/delete probe;
6. Temporal worker and recurring workflow bootstrap;
7. Telegram polling runner.

A failed dependency therefore fails the container before it can report ready.

## HTTP and telemetry

Stable REST paths are unchanged. The detailed access matrix is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Unknown routes are default-deny:
anonymous requests receive `401`, authenticated requests receive `404`/`405`.

`POST /api/client-events` accepts privacy-minimized browser activity batches,
returns `204` even for malformed input and logs only normalized fields. Raw form
values, addresses and passwords are not persisted. Loki query:

```logql
{app="my-utils-api"} | json | event_type="client_event"
```

### Apple Health import

The iOS Shortcut imports daily steps through `POST /api/health/steps` and body
weight through `POST /api/health/weight/import`. It sends grouped daily values
as a multiline string in the empty JSON key; the final line is today. Blank or
zero lines preserve missing calendar days.

## Telegram agent and Temporal

Telegram uses long polling. OpenRouter handles the tool loop; every message and
tool result is stored in PostgreSQL. Old dialogs are compressed into one rolling
summary automatically while the configured recent tail stays verbatim.

All Go workers poll task queue `myutils-go-v1`. Workflow IDs use the `go-v1-`
generation marker. Startup deliberately does not query, terminate, signal or
reuse histories created by the removed Kotlin worker.

For every allowed Telegram user, the weekly workflow sends 90-day steps and
body-weight PNGs each Saturday at 12:00 in `temporal.zone-id`. Steps include a
table for the latest ten calendar days with placeholders; weight includes the
latest ten actual measurements.

| Variable | Description |
| --- | --- |
| `MYUTILS_TELEGRAM_ENABLED` | Enable Bot API integration |
| `MYUTILS_TELEGRAM_POLLING_ENABLED` | Enable inbound long polling |
| `TELEGRAM_BOT_TOKEN` | Bot token |
| `TELEGRAM_ALLOWED_USER_IDS` | Comma-separated Telegram user IDs |
| `TELEGRAM_FILE_UPLOAD_TOKEN` | Optional override for file delivery |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `OPENROUTER_PROXY_*` | Optional HTTP proxy for OpenRouter |
| `MYUTILS_TEMPORAL_ENABLED` | Enable Temporal workers |
| `TEMPORAL_TARGET` | Temporal frontend address |

Model, retry policy, recent memory, compaction threshold and reminders are
runtime settings in PostgreSQL. Change existing values only through
`PUT /api/admin/settings/{key}` and read them back.

### Agent test console

`/api/admin/agent-test-chats/**` creates isolated sandbox conversations through
the same LLM/tool loop. Sandbox tools never read or mutate real workout,
body-weight, fact, notification or Telegram data.

### Send a file through Telegram

`POST /api/telegram/files` accepts multipart `file` and optional `caption`, up
to 20 MB. `X-Telegram-File-Token` must match the configured token; if the
override is empty it is derived from the bot token.

## Configuration

See [.env.example](.env.example). Important variables:

| Variable | Default |
| --- | --- |
| `POSTGRES_HOST` / `POSTGRES_PORT` | `localhost` / `5432` |
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | `myutils` |
| `REDIS_HOST` / `REDIS_PORT` | `localhost` / `6379` |
| `MYUTILS_JWT_SECRET` | development-only built-in value |
| `MYUTILS_JWT_EXPIRATION_HOURS` | `24` |
| `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY` | required for recoverable client keys |

Set `MYUTILS_ENV=production` together with an explicit secret of at least 32
bytes; production mode refuses the development JWT default.

## Deployment

Push to `main` starts Woodpecker: tests and vet run first, then the server-side
deploy script builds the multi-stage Go image. `docker-compose.jenkins.yml`
keeps a historical filename; Jenkins is not the active CI.

| File | Purpose |
| --- | --- |
| `docker-compose.yml` | local API + Temporal |
| `docker-compose.bundled.yml` | add local PostgreSQL + Redis |
| `docker-compose.dev.yml` | infrastructure for host Go process |
| `docker-compose.jenkins.yml` | production topology |
| `docker-compose.utils.yml` | shared/utility deployment variant |

## WireGuard relay

`/api/admin/wireguard/**` stores relay desired state and encrypts recoverable
private keys with AES-256-GCM. The host agent uses a relay-scoped hashed token
on `/api/internal/wireguard/relays/{id}/**`. Operational scripts and the safe
installation procedure live in [ops/wireguard/README.md](ops/wireguard/README.md).
Every heartbeat converts cumulative WireGuard and routing counters into interval
deltas, persists the current download/upload rate, and retains traffic samples
for period summaries. `GET .../peers?range=HOUR|DAY|WEEK|MONTH` returns the
persisted rates and per-peer traffic for the selected period; the peer metrics
endpoint returns the same period summary together with chart buckets.
`GET .../relays/{id}/snapshot?range=HOUR|DAY|WEEK|MONTH` is the dashboard read
model: one request returns the relay, every peer, all compact traffic previews,
and persisted healthcheck history for both AWG exits. Exit samples are recorded
from validated agent heartbeats and retained for 31 days.
