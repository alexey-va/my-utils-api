# my-utils-api — architecture (short)

Полная шпаргалка для агентов: **`AGENTS.md`** в корне репо. Здесь — сжатая схема для людей.

## Stack

Kotlin 2.1 · Spring Boot 3.4 · Postgres 16 · Redis 7 · Temporal 1.30 · LangChain4j → OpenRouter · Telegram long polling.

## Runtime and deployment

```bash
docker compose up -d --build          # local :8080
# production topology: docker-compose.jenkins.yml → :18080
```

Push в `main` запускает `.woodpecker.yml`, который вызывает серверный deploy
script. `docker-compose.jenkins.yml` сохраняет историческое имя и описывает
production topology; наличие этого имени не означает, что Jenkins является
текущим CI.

| Service | Local | Production host (127.0.0.1) |
|---------|-------|---------------------|
| api | 8080 | 18080 |
| postgres | 5432 | 15432 |
| redis | 6379 | 16379 |
| temporal gRPC | 7233 | 17233 |
| temporal-ui | 8233 | 18233 |

Prod UI: https://utils.alexeyav.ru · Temporal: https://temporal.alexeyav.ru · Logs: Grafana `/grafana/` → Loki `{app="my-utils-api"}`.
Promtail сохраняет исходную JSON-строку приложения; поля route telemetry
доступны через `{app="my-utils-api"} | json | event_type="client_event"`.

## Data

- **Postgres**: `users`, `exercises`, `workout_entries`, `app_settings`
- Дневник всегда от пользователя `local@workout` (общий для web + Telegram)
- Запись: `вес 3*X/МАХ` → `weight_kg`, `set_count=3`, `reps_per_set`, `max_reps`
- **Redis**: JWT sessions `myutils:session:{id}`; LangChain4j chat memory per `chatId`

## HTTP security

| Path | Access |
|------|--------|
| `/api/health`, `/api/auth/login` | public |
| `/api/workouts/**` | public (личный инстанс) |
| `/api/health/steps`, `/api/health/weight` | public GET/POST |
| `POST /api/telegram/files` | public security route + mandatory `X-Telegram-File-Token` application check |
| `POST /api/client-events` | public, normalized browser telemetry with request IP/User-Agent; no raw form values |
| `/api/admin/settings/**` | `ADMIN` |
| `/api/admin/agent-memory/**` | `ADMIN` |
| `/api/auth/**` (else) | JWT + Redis session |
| rest | deny |

Таб-пароль во frontend — только клиентский visibility gate. Источник истины
для серверного доступа: `infra/security/SecurityConfig.kt`. Если политика
меняется, нужно синхронно обновить эту таблицу, frontend flow и тесты.

## Telegram → agent

```
getUpdates → WorkoutAgentService
  ├─ Temporal on  → WorkoutAgentWorkflow (durable)
  └─ Temporal off → WorkoutLangChain4jAgent.run()
```

**Workflow loop**: `resolvePrelude` → (`llmStep` ↔ `executeTool` × N) → Telegram reply.

**Tools** (LangChain4j `@Tool` / `WorkoutToolsService.runTool`): `listExercises`, `logWorkout`, `createExercise`, `renameExercise`, `deleteWorkout`, progress/summary, optional Temporal notifications.

## Temporal workflows

| Workflow | ID pattern | Purpose |
|----------|------------|---------|
| `WorkoutAgentWorkflow` | per turn | Agent + tools |
| `EveningWorkoutReminderWorkflow` | `evening-reminder-{chatId}` | Daily nudge |
| `TelegramNotificationWorkflow` | `tg-notify-{chatId}-{uuid}` | Delayed message |

Task queue: `myutils-main`. Kotlin DTOs need `TemporalDataConverterConfiguration` (Jackson).

## Runtime settings

`properties/Properties.kt` — keys in `app_settings`, reload ~1 min. Admin:
`PUT /api/admin/settings/{key}`; current access policy is listed above.
Evening reminder toggles reschedule Temporal workflows on apply.

## Dev

```bash
docker compose -f docker-compose.dev.yml up -d   # infra
cp .env.example .env && ./gradlew bootRun
./gradlew test                                    # Docker PG+Redis required
```

Observability configs: `observability/` · Deploy runbook: `../jenkins/DEPLOY-alexeyav.md`.
