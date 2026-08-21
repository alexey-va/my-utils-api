# my-utils frontend/backend contract

`my-utils` и `my-utils-api` образуют один продукт, но находятся в независимых
Git-репозиториях. Этот документ описывает границу между ними; внутренняя
архитектура backend находится в [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Ownership

| Concern | Frontend (`my-utils`) | Backend (`my-utils-api`) |
| --- | --- | --- |
| UI routes and tabs | `src/config/featureCatalog.tsx` | — |
| REST paths | `src/api/endpoints.ts` | `internal/httpapi/` |
| Client payload types | `src/api/types.ts`, feature API modules | request/response structs in `internal/httpapi/` and services |
| Authentication state | access JWT + transparent `401` refresh | JWT plus hashed Redis refresh session |
| Workout UI/data fetching | `src/features/workout/` | `WorkoutController`, `WorkoutService` |
| Runtime settings UI | `src/features/properties/` | `AdminSettingsController` |
| Agent-memory UI | `src/features/agents/` | `AdminAgentMemoryController` |
| Grafana/Temporal embeds | `src/config/grafana.ts`, `temporal.ts` | production infrastructure |

## Stable production paths

| Browser path | Destination |
| --- | --- |
| `/` | SPA workout page |
| `/api/**` | Go API |
| `/grafana/**` | Grafana |
| `/temporal/**` | Temporal UI |
| `/workflows` | SPA page embedding `/temporal/` |

Frontend endpoint constants already begin with `/api/`. In production use an
empty `VITE_API_BASE_URL`; a value ending in `/api` produces duplicated
`/api/api/...` paths.

## Access model

- `requiresTabPassword` is a client-side gate.
- `requiresAuth` controls the frontend login flow.
- Only route middleware in `internal/httpapi/` determines backend authorization.
- Current public/protected paths are documented in `ARCHITECTURE.md`.

Do not describe a route as protected based only on the sidebar or page wrapper.

## Cross-repo change workflow

1. Define the backend path and DTO.
2. Implement service/handler changes and a focused backend test.
3. Update `src/api/endpoints.ts` and frontend types.
4. Update the consuming page or hook.
5. Run `go test ./...` plus `go vet ./...` in backend and `npm run build` in frontend.
6. Review and commit each repository separately.

## Local development

```bash
# backend
cd utils/my-utils-api
docker compose up -d --build

# frontend, separate terminal
cd utils/my-utils
npm run dev
```

The frontend dev server proxies `/api` to `http://localhost:8080`.

## Deployment

Each repository owns its `.woodpecker.yml`. Push to `main` deploys only that
repository:

- frontend push rebuilds and deploys the SPA;
- backend push rebuilds and deploys API/runtime services.

If a feature changes both sides, verify both locally before either production
push. A successful push is not proof that the sibling repository was deployed.
