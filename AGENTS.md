# AGENTS.md — my-utils-api

Kotlin **Spring Boot 3.4**, JVM 21. Postgres + Redis + Temporal + LangChain4j (OpenRouter). **Docker-first.**

This is an independent Git repository. Frontend changes belong in
`../my-utils` and must be committed and verified separately.

## Layout

```
dev.myutils.api/
├── web/              — REST controllers + DTOs
├── service/          — WorkoutService, AuthService, WorkoutBotFacade
├── domain/           — JPA entities + repositories
├── agent/            — Telegram agent: tools, context, LangChain4j
│   └── langchain/    — WorkoutLangChain4jAgent, tools, Redis memory
├── telegram/         — long polling, messenger, inbound coalescer
├── temporal/         — workflows + activities (queue: myutils-main)
│   ├── agent/        — WorkoutAgentWorkflow, llmStep, WorkoutToolActivities
│   ├── reminder/     — evening reminder workflow
│   └── notification/ — scheduled Telegram messages
├── properties/       — runtime settings (app_settings table)
├── infra/            — config, security, session, http, util, openrouter, web helpers
└── observability/    — Loki/Promtail/Grafana provisioning (server-side)
```

Flyway: `src/main/resources/db/migration/`. Tests: `src/test/kotlin/.../testkit/`.

## Commands

| Task | Command |
|------|---------|
| Full stack | `docker compose up -d --build` |
| Rebuild API | `docker compose up -d --build api` |
| Infra only | `docker compose -f docker-compose.dev.yml up -d` then `./gradlew bootRun` |
| Tests | `./gradlew test` (needs Docker Postgres+Redis) |
| Prod topology | `docker-compose.jenkins.yml` — API `18080`, Loki via Promtail labels; filename is historical |

## Telegram agent flow

1. `TelegramBotRunner` → `TelegramInboundCoalescer` → `WorkoutAgentService.handleMessage`
2. If Temporal enabled → `TemporalWorkflowService.startAgentTurn` (async workflow)
3. Else → `WorkoutLangChain4jAgent.run()` (inline LangChain4j tool loop)

**Temporal workflow** (`WorkoutAgentWorkflowImpl`):

1. `resolvePrelude` — access check, `/start` (no LLM)
2. Loop: `llmStep` → if tool calls → `WorkoutToolActivities.executeTool` each → `recordToolResults`
3. `TelegramActivities.sendMessage` with final reply

**Direct path**: `AiServices` + `@Tool` methods on `WorkoutLangChainTools` — tools run inside JVM.

**Tool naming trap**: LLM emits camelCase (`logWorkout`); `WorkoutToolsService.runTool` expects snake_case — normalization is in `runTool`, don't bypass it.

## Key conventions

- Workout diary user: fixed `local@workout` (`WorkoutService.LOCAL_WORKOUT_EMAIL`)
- `/api/workouts/**`, health steps/weight, admin settings, and admin
  agent-memory are currently `permitAll`
- `/api/auth/**` except login needs JWT + Redis session
- Client-side tab passwords are not backend authorization. Check
  `infra/security/SecurityConfig.kt` before documenting an endpoint as
  protected.
- Bot beans: `@ConditionalOnTelegramBot` (needs `TELEGRAM_BOT_TOKEN`)
- Temporal beans: `@ConditionalOnProperty(myutils.temporal.enabled=true)`
- Runtime tunables: `properties/Properties.kt` → `AppProperties.*`, admin API `PUT /api/admin/settings/{key}`
- Package moves: shared stuff lives under `infra/`, not top-level `config/`

## Testing

```kotlin
@MyUtilsSpringTest(environment = Environment.TESTING)
class MyTest : TestingIntegrationTestBase() { ... }
```

- `Environment.TESTING` → profile `testing`, `@Primary` fakes in `testkit/impl/TestingClients.kt` (no HTTP to OpenRouter/Telegram)
- `Environment.PRODUCTION` → real beans, same as prod wiring
- Temporal: `TemporalWorkflowTests` with in-process `TestWorkflowEnvironment`
- Don't use `@Profile("!testing")` on prod beans — override with `@Primary` in test config

## Add a feature (checklist)

1. Flyway migration if schema changes
2. `domain/` → `service/` → `web/` controller
3. `SecurityConfig` if new route pattern
4. Mirror path in `../my-utils/src/api/endpoints.ts` if UI needs it
5. `./gradlew test` then `docker compose up -d --build api`

Frontend consumers must be updated separately in
`../my-utils/src/api/endpoints.ts` and its API types.

## Add an agent tool

1. Method on `WorkoutLangChainTools` with `@Tool`
2. Delegate to `WorkoutToolsService` (single implementation)
3. Add snake_case branch in `WorkoutToolsService.runTool` (Temporal path uses this)
4. Test via `WorkoutToolsServiceTest` or agent integration test

## Observability

- App logs: stdout → Promtail (`logging=promtail`, `app=my-utils-api` labels on `api` service)
- Grafana: `{app="my-utils-api"}`, dashboard uid `myutils-api-logs`
- Sync server configs: `observability/sync-to-server.sh`

Human-readable details: `docs/ARCHITECTURE.md` (keep in sync when changing flows).

## Verification and deployment

Minimum local gate:

```bash
./gradlew test
git diff --check
```

Tests require local PostgreSQL and Redis; use `docker-compose.dev.yml`. Testing
beans replace Telegram/OpenRouter clients, so tests must not make real external
calls.

Push to `main` triggers the Woodpecker production pipeline. Do not push merely
to verify a change. Production restarts, secret changes, firewall changes, and
observability syncs are separate operational actions.
