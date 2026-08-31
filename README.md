# my-utils-api

Go REST API for [my-utils](https://github.com/alexey-va/my-utils).

**Для разработки:** [AGENTS.md](AGENTS.md) ·
**Документация:** [docs/README.md](docs/README.md) ·
**Архитектура:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Stack

- **Go 1.26** — один статический application binary;
- **PostgreSQL 16** — users, workout, health, settings, agent memory; SQL-миграции совместимы с существующей таблицей Flyway history;
- **Redis 7** — JWT access sessions and sliding refresh sessions;
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

Telegram uses long polling and accepts text or voice messages. Allowed voice
messages are downloaded with a 20 MB limit, transcribed through OpenRouter STT,
then passed into the same serial agent turn as text. Every message and tool
result is stored in PostgreSQL. Old dialogs are compressed into one rolling
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

Agent model, `openrouter.transcription-model`, retry policy, recent memory,
compaction threshold and reminders are runtime settings in PostgreSQL. Change
existing values only through `PUT /api/admin/settings/{key}` and read them back.

### WireGuard self-service bot

The optional VPN bot uses a separate Telegram bot token and polling runner in
the same API process. It is a deterministic menu application: it never invokes
OpenRouter, the workout agent, or free-text tool selection.

- A new private-chat user submits an access request. Every configured admin is
  notified and must approve or reject it explicitly.
- Approved users can create, list, reissue and delete only their own tunnels,
  download protected `.conf` files and QR codes, and view 30-day traffic totals.
- The default limit is one tunnel. An admin can set 1, 2, 3 or 5 from the bot;
  the database contract permits up to 10.
- Configured bot admins also have a personal “My tunnels” area. Their own
  tunnel count is unlimited; user limits and approval checks remain unchanged.
- Blocking a user disables every owned WireGuard peer. Re-approval enables the
  same peers again. All access, credential-delivery and mutation actions are
  recorded in the VPN-bot audit table without private key material.
- Group chats are rejected. Ownership is checked in PostgreSQL for every peer
  operation, even when callback data contains a valid peer UUID.

The feature is disabled by default. Create a separate bot with BotFather, then
set these deployment secrets/values and restart through the normal pipeline:

| Variable | Purpose |
| --- | --- |
| `MYUTILS_VPN_TELEGRAM_ENABLED` | Enable the separate VPN bot |
| `MYUTILS_VPN_TELEGRAM_POLLING_ENABLED` | Enable its inbound long polling |
| `VPN_TELEGRAM_BOT_TOKEN` | Separate BotFather token; must differ from the workout bot token |
| `VPN_TELEGRAM_ADMIN_USER_IDS` | Comma-separated Telegram IDs allowed to approve and manage users |
| `VPN_TELEGRAM_RELAY_ID` | Existing WireGuard relay used for self-service peers |

Do not commit or paste the token into configuration files. Startup fails closed
when the bot is enabled without a token, admin IDs or relay ID. After enabling,
each configured admin must open the new bot and press `/start` once; Telegram
does not allow a bot to initiate a private chat before that.

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
| `MYUTILS_REFRESH_SESSION_DAYS` | `30` |
| `MYUTILS_REFRESH_COOKIE_NAME` | `myutils_refresh_session` |
| `MYUTILS_REFRESH_COOKIE_SECURE` | `true` in production mode; production overlay sets it explicitly |
| `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY` | required for recoverable client keys |

Set `MYUTILS_ENV=production` together with an explicit secret of at least 32
bytes; production mode refuses the development JWT default.

The SPA keeps the short-lived access JWT locally. Login also sets an HttpOnly
refresh cookie backed by a hashed Redis session. A protected request that gets
`401` refreshes once and retries transparently; active refresh sessions slide
for 30 days, while an actually expired or revoked session returns the user to
the login page.

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
`PUT .../relays/{id}/exit-preference` accepts `AUTO`, `PRIMARY`, or `SECONDARY`.
The preference is delivered through agent desired state and keeps a safe
fallback to the other healthy exit.
Peer metadata is managed independently from WireGuard key material: `PATCH
.../relays/{id}/peers/{peerId}` updates the display name, category, or enabled
state, while `PUT .../relays/{id}/peers/order` atomically persists the complete
ordered peer list and its categories. Categories are first-class relay records:
`POST/PATCH/DELETE .../relays/{id}/categories` manages empty and populated
categories, and `PUT .../relays/{id}/categories/order` persists their complete
order. Renaming a category updates every peer in it atomically; a non-empty
category cannot be deleted. Deleting a peer also removes its retained traffic
samples in the same transaction.

The Prometheus endpoint exports relay readiness, routing health, agent
freshness, per-exit health/selection/latency, configured preference, and Internal/External packet loss
and RTT. Versioned Grafana provisioning under `observability/` alerts through
the existing Discord receiver for a stale agent, broken routing, both exits
down, primary degradation, reserve use, and sustained packet loss. The
recovery playbook and its encrypted-key workflow live in
[`ops/wireguard/ansible/`](ops/wireguard/ansible/README.md).
