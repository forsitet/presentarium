# Presentarium

Веб-сервис для интерактивных опросов и викторин в реальном времени — аналог Mentimeter / Kahoot.
Организатор создаёт опрос, открывает «комнату», участники подключаются по короткому коду
(или QR-коду) и отвечают на вопросы синхронно через WebSocket.

## Возможности

- **Типы вопросов**: `single_choice`, `multiple_choice`, `image_choice`, `open_text`,
  `word_cloud`, `brainstorm` (идеи + голосование).
- **Живые сессии**: один WebSocket-хаб на комнату, реплей состояния при reconnect,
  таймер вопроса, лидерборд, live-распределение ответов.
- **Презентации**: загрузка `.pptx`, конвертация в слайды, синхронный показ слайдов
  участникам параллельно с вопросами.
- **Модерация**: организатор может скрывать отдельные ответы и идеи в брейншторме.
- **История и экспорт**: список прошедших сессий, экспорт результатов в CSV и PDF.
- **Аутентификация**: JWT (access + refresh), регистрация, восстановление пароля по email.
- **Хранилище медиа**: S3-совместимое (MinIO в dev, любой S3 в проде) — публичный
  бакет для картинок и слайдов, приватный для исходников `.pptx` и экспортов.

## Стек

**Backend** (`backend/`): Go 1.24, [chi](https://github.com/go-chi/chi),
[sqlx](https://github.com/jmoiron/sqlx), [gorilla/websocket](https://github.com/gorilla/websocket),
[golang-migrate](https://github.com/golang-migrate/migrate), aws-sdk-go-v2 (S3 API),
[golang-jwt](https://github.com/golang-jwt/jwt).

**Frontend** (`frontend/`): React 19, TypeScript, Vite 6, Tailwind CSS, Zustand,
react-router 7, Recharts, react-hook-form + Zod, qrcode.react.

**Инфраструктура**: PostgreSQL 16, MinIO, nginx, Docker Compose.

## Быстрый старт

Требуется Docker и Docker Compose.

```bash
cp .env.example .env
docker compose up -d
```

После старта:

- Frontend / приложение: <http://localhost> (через nginx)
- Backend API: <http://localhost:8080>
- MinIO console: <http://localhost:9001> (логин/пароль из `.env`)
- Postgres: `localhost:5432`

Миграции прогоняются автоматически при первом старте backend-контейнера.

### Локально без Docker

```bash
# Postgres + MinIO в контейнерах, остальное на хосте
docker compose up -d postgres minio minio-init

# Backend
cd backend
go run ./cmd/migrate up
go run ./cmd/server

# Frontend (в другом терминале)
cd frontend
npm install
npm run dev
```

При локальном запуске backend на хосте оставь `S3_ENDPOINT=http://localhost:9000`
в `.env` — compose-файл переопределяет это значение только для сервиса в сети docker.

## Структура

```
backend/
  cmd/server/        — точка входа HTTP/WS-сервера
  cmd/migrate/       — раннер миграций
  internal/handler/  — HTTP-роуты (chi)
  internal/service/  — бизнес-логика
  internal/repository/ — слой доступа к БД
  internal/ws/       — WebSocket hub, room, клиенты
  internal/storage/  — обёртка над S3 API
  migrations/        — SQL-миграции (up/down)
  test/integration/  — интеграционные тесты
frontend/
  src/pages/         — страницы (login, dashboard, host, participant, results)
  src/components/    — UI-компоненты (вопросы, графики, word cloud, brainstorm)
  src/ws/            — WebSocket-клиент
  src/stores/        — Zustand stores
loadtest/            — k6-сценарии
nginx/               — конфиги для dev и prod
docker-compose.yml          — dev-стек
docker-compose.prod.yml     — prod-стек
DEPLOY.md            — runbook продового деплоя на VPS
```

## Конфигурация

Все настройки — переменные окружения. Полный список с комментариями смотри
в [`.env.example`](.env.example). Ключевые группы:

- `DB_*` — параметры PostgreSQL.
- `JWT_SECRET`, `JWT_ACCESS_TTL`, `JWT_REFRESH_TTL` — токены.
- `S3_*` — endpoint и креды MinIO/S3, имена бакетов, public base URL.
- `SMTP_*` — отправка писем для восстановления пароля.
- `FRONTEND_URL` — origin для CORS.

## API

Базовый префикс — `/api`. Все защищённые эндпоинты требуют `Authorization: Bearer <jwt>`.
Healthcheck — `GET /health`.

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/auth/register` | Регистрация |
| POST | `/auth/login` | Логин (выдаёт access + refresh cookie) |
| POST | `/auth/refresh` | Обновление access-токена |
| POST | `/auth/logout` | Выход |
| POST | `/auth/forgot-password` | Запрос письма для сброса |
| POST | `/auth/reset-password` | Сброс по токену |
| CRUD | `/polls`, `/polls/{id}/questions` | Опросы и вопросы |
| POST | `/polls/{id}/copy` | Дублировать опрос |
| POST | `/upload/image` | Загрузка картинки |
| CRUD | `/presentations` | Загрузка и листинг `.pptx` |
| POST | `/rooms` | Открыть комнату |
| GET | `/rooms/{code}` | Состояние комнаты |
| PATCH | `/rooms/{code}/state` | Смена состояния (lobby/active/finished) |
| GET | `/sessions` | История сессий организатора |
| GET | `/sessions/{id}/export/csv` | Экспорт CSV |
| POST | `/sessions/{id}/export/pdf` | Экспорт PDF |
| PATCH | `/sessions/{id}/answers/{answerId}` | Скрыть/показать ответ |
| WS | `/ws/room/{code}` | WebSocket подключения участника/хоста |

## Тесты

```bash
# unit + integration (requires running Postgres)
cd backend
go test ./...

# frontend lint/build
cd frontend
npm run lint
npm run build
```

## Нагрузочное тестирование

В `loadtest/` лежат сценарии для [k6](https://k6.io):

- `room_load_test.js` — базовый сценарий комнаты с N участниками.
- `stress_test.js` — пошаговый stress-тест для определения breakpoint'а.

Запуск:

```bash
k6 run loadtest/room_load_test.js -e VUS=100
```

Подробнее — в [`loadtest/README.md`](loadtest/README.md).

## Деплой

Прод-конфигурация и пошаговый runbook на VPS — в [`DEPLOY.md`](DEPLOY.md).
Используется отдельный `docker-compose.prod.yml`, образы публикуются в GHCR через
GitHub Actions (`.github/workflows/deploy.yml`).

## Лицензия

Проект разрабатывается как дипломная работа. Использование исходного кода —
по согласованию с автором.
