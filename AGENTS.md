# AGENTS.md — my-utils-api

Kotlin **Spring Boot 3** API for my-utils. **Postgres** + **Redis**. Runnable as a **Docker** image.

## Layout

```
src/main/kotlin/dev/myutils/api/
├── domain/          — JPA entities + repositories
├── session/         — Redis SessionService
├── service/         — AuthService, WorkoutService, WorkoutBotFacade
├── agent/           — OpenRouter tool-calling agent (Telegram)
├── telegram/        — Webhook + Telegram API client
├── openrouter/      — Chat completions client
├── security/        — JWT filter + SecurityConfig
├── web/             — REST controllers
└── config/
src/main/resources/db/migration/   — Flyway SQL
Dockerfile / docker-compose.yml    — container build + full stack
```

## Commands

| Task | Command |
|------|---------|
| Full stack | `docker compose up -d --build` |
| Rebuild + restart API | `docker compose up -d --build api` |
| API logs | `docker compose logs -f api` |
| Infra only (Gradle on host) | `docker compose -f docker-compose.dev.yml up -d` |
| Run on host | `./gradlew bootRun` |
| Test | `./gradlew test` |

Port **8080**. Image tag: `my-utils-api:local`.

## Adding features later

1. Flyway migration in `db/migration/`
2. Domain + service + controller
3. Update `SecurityConfig` route rules
4. Mirror paths in `../my-utils/src/api/endpoints.ts`
5. Rebuild container: `docker compose up -d --build api`
