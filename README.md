# my-utils-api

Kotlin/Spring Boot REST API for [my-utils](https://github.com/alexey-va/my-utils).

## Stack

- **PostgreSQL** — users, exercises, workout entries (Flyway)
- **Redis** — JWT sessions
- **Docker** — full stack in one command

## Run everything in Docker (recommended)

Build and start API + Postgres + Redis:

```bash
docker compose up -d --build
```

| Service  | URL |
|----------|-----|
| API      | http://localhost:8080 |
| Postgres | localhost:5432 (`myutils` / `myutils`) |
| Redis    | localhost:6379 |

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
| `TELEGRAM_WEBHOOK_SECRET` | Production only — random string in webhook URL path |
| `TELEGRAM_WEBHOOK_BASE_URL` | Production only — public API URL (e.g. `https://utils.alexeyav.ru`) |

**Local dev:** `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USER_IDS`, `OPENROUTER_API_KEY` in `.env`. No webhook vars — the API uses **long polling** (`getUpdates`).

**Production:** also set `TELEGRAM_WEBHOOK_BASE_URL` and `TELEGRAM_WEBHOOK_SECRET`. On startup the API registers:

`{TELEGRAM_WEBHOOK_BASE_URL}/api/telegram/webhook/{TELEGRAM_WEBHOOK_SECRET}`

Example message: `bench 80kg 3x5` or `сегодня присед 100 на 5х5`.

**Get your Telegram user id:** message [@userinfobot](https://t.me/userinfobot) or inspect webhook updates in logs during testing.
