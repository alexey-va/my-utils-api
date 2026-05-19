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

Infra in Docker, API via Gradle:

```bash
docker compose -f docker-compose.dev.yml up -d
./gradlew bootRun
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
