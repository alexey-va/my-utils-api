# AGENTS.md — utils (monorepo)

Два репозитория под `utils/`: **SPA** + **API**. Деплой на `utils.alexeyav.ru` через **Woodpecker CI** (`git push origin main`).

## Репозитории

| Путь | GitHub | Назначение |
|------|--------|------------|
| `my-utils/` | alexey-va/my-utils | Vite + React + Refine, порт 13082 |
| `my-utils-api/` | alexey-va/my-utils-api | Kotlin Spring Boot, порт 18080 |
| `jenkins/` | в my-utils-api или локально | nginx, job XML, DEPLOY-alexeyav.md |
| `observability/` | в my-utils-api | Loki/Promtail/Grafana configs |

**Читай модульный AGENTS.md** — там детали разработки. Этот файл — только карта.

## Прод

| URL | Что |
|-----|-----|
| https://utils.alexeyav.ru | UI + `/api` |
| https://temporal.alexeyav.ru | Temporal UI |
| https://utils.alexeyav.ru/grafana/ | Grafana (логи API в Loki) |

Woodpecker: `.woodpecker.yml` в каждом репо. Сервер: SSH host `Timeweb`.

## Типичные задачи

| Задача | Где |
|--------|-----|
| Новая вкладка UI | `my-utils/AGENTS.md` → `config/features.tsx` |
| REST endpoint | `my-utils-api/AGENTS.md` → web + service + Flyway |
| Telegram / агент | `my-utils-api` → `agent/`, `temporal/agent/` |
| Тесты API | `@MyUtilsSpringTest(environment = TESTING)` + testkit |
| Логи в Grafana | Loki `{app="my-utils-api"}`, дашборд `myutils-api-logs` |
| Деплой | `git push origin main` → Woodpecker |

## Не путать

- **Логи** → Loki (Promtail собирает stdout Docker). **Метрики** → Prometheus.
- **Temporal tool loop**: отдельные activities (`llmStep`, `executeTool`), не один `runAgent`.
- **LangChain4j tool names**: camelCase (`logWorkout`); `WorkoutToolsService.runTool` нормализует в snake_case.
