# my-utils-api

Kotlin/Spring Boot REST API for [my-utils](https://github.com/alexey-va/my-utils).

**Подробная архитектура:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — стек, БД, авторизация, Telegram-бот, Temporal, деплой.

## Stack

- **PostgreSQL** — users, exercises, workout entries (Flyway)
- **Redis** — JWT sessions
- **Temporal** — durable workflows (evening workout reminders, future automations)
- **Docker** — full stack in one command

## Run everything in Docker (recommended)

Build and start API + Postgres + Redis:

```bash
DOCKER_BUILDKIT=1 docker compose up -d --build
```

Docker build uses `gradle:9.4.1-jdk21` (no wrapper zip download each time). Local dev still uses `./gradlew`.

| Service  | URL |
|----------|-----|
| API      | http://localhost:8080 |
| Postgres | localhost:5432 (`myutils` / `myutils`) |
| Redis    | localhost:6379 |
| Temporal | `localhost:7233` (gRPC) |
| Temporal UI | http://localhost:8233 |

**Restart API after code changes:**

```bash
docker compose up -d --build api
```

**Restart without rebuild** (config-only):

```bash
docker compose restart api
```

**Logs:**

```bash
docker compose logs -f api
```

**Stop:**

```bash
docker compose down
```

Health: `GET http://localhost:8080/api/health`

### Temporal

Workers and workflows live in `dev.myutils.api.temporal`. Task queue: `myutils-main`.

| Env | Default (Docker) | Description |
|-----|------------------|-------------|
| `MYUTILS_TEMPORAL_ENABLED` | `true` | Connect workers to Temporal |
| `TEMPORAL_TARGET` | `temporal:7233` | Temporal frontend address |

Evening reminder, model, TTL — **runtime settings** in Postgres (`PUT /api/admin/settings/{key}`, JWT). See `docs/ARCHITECTURE.md`.

**Host dev** (`./gradlew bootRun`): infra + Temporal via `docker compose -f docker-compose.dev.yml up -d`, set `TEMPORAL_TARGET=127.0.0.1:7233` in `.env`.

**Jenkins** deploy starts Temporal + UI (`127.0.0.1:17233` gRPC, `127.0.0.1:18233` UI) with workers enabled. Agent tools: `send_notification`, `schedule_notification`, `cancel_notification`.

## Local development (Gradle on host)

Copy secrets (gitignored):

```bash
cp .env.example .env
# edit .env — TELEGRAM_BOT_TOKEN, OPENROUTER_API_KEY, TELEGRAM_ALLOWED_USER_IDS, etc.
```

Infra in Docker, API via Gradle (loads `.env` via `spring.config.import`):

```bash
docker compose -f docker-compose.dev.yml up -d
./gradlew bootRun
```

Or full stack in Docker (reads `.env`):

```bash
docker compose up -d --build
```

## Tests

Requires Postgres + Redis on localhost (use either compose file):

```bash
docker compose -f docker-compose.dev.yml up -d
./gradlew test
```

## Dev login

| Email | Password |
|-------|----------|
| `dev@example.com` | `password` |

Workout tab uses shared `local@workout` (no sign-in required).

## Configuration

Environment variables (set in `docker-compose.yml` for the `api` service):

| Variable | Default |
|----------|---------|
| `POSTGRES_HOST` | `localhost` |
| `POSTGRES_PORT` | `5432` |
| `POSTGRES_DB` | `myutils` |
| `POSTGRES_USER` | `myutils` |
| `POSTGRES_PASSWORD` | `myutils` |
| `REDIS_HOST` | `localhost` |
| `REDIS_PORT` | `6379` |

JWT secret: `myutils.jwt.secret` in `application.yml` — override in production.

## Telegram workout bot (optional)

Log workouts by messaging a Telegram bot. Messages are parsed by an OpenRouter model that calls tools to write into the same workout log as the web UI.

| Variable | Description |
|----------|-------------|
| `TELEGRAM_BOT_TOKEN` | From [@BotFather](https://t.me/BotFather) — bot starts when this is set |
| `TELEGRAM_ALLOWED_USER_IDS` | Your Telegram user id (comma-separated) |
| `OPENROUTER_API_KEY` | [OpenRouter](https://openrouter.ai/) API key |
| `OPENROUTER_PROXY_*` | Optional HTTP proxy for Telegram + OpenRouter (see `.env.example`) |

Set `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USER_IDS`, and `OPENROUTER_API_KEY` in `.env`. The API receives messages via **long polling** (`getUpdates`) — no public URL required.

Example message: `bench 80kg 3x5` or `сегодня присед 100 на 5х5`.

**Get your Telegram user id:** message [@userinfobot](https://t.me/userinfobot).
