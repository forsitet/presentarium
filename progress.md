# Progress Log — ВП ПИО

## Статус проекта

**Всего задач:** 46
**Выполнено:** 9
**В работе:** 0
**Ожидает:** 37

## Сессии работы агентов

### 2025-09-03 10:00 — TASK-001: Инициализация структуры репозитории и Docker Compose
**Что сделано:**
- Созданы директории: backend/, frontend/, nginx/, .github/workflows/
- backend/cmd/server/main.go: Go stub-сервер, отвечает GET /health → {"status":"ok"}
- backend/go.mod: модуль presentarium, go 1.23
- backend/Dockerfile: multi-stage (golang:1.23-alpine + alpine:3.19)
- docker-compose.yml: PostgreSQL 16 (порт 5432), backend (порт 8080), nginx (порт 80)
- nginx/nginx.conf: reverse proxy /api/, /ws/, /uploads/, frontend; gzip; WebSocket upgrade
- .env.example: все переменные окружения с описанием
- .gitignore

**Изменённые файлы:**
- backend/cmd/server/main.go (новый)
- backend/go.mod (новый)
- backend/Dockerfile (новый)
- docker-compose.yml (новый)
- nginx/nginx.conf (новый)
- .env.example (новый)
- .gitignore (новый)

**Коммит:** 08d8db7
**Статус:** done

**Следующая задача:** TASK-002 (зависимости выполнены — TASK-001 done) или TASK-004 (зависимости выполнены)

---

### 2025-10-14 09:00 — TASK-002: Go-модуль с clean architecture и базовой конфигурацией
**Что сделано:**
- backend/go.mod: добавлены все зависимости (chi, gorilla/websocket, sqlx, lib/pq, golang-migrate, golang-jwt/jwt/v5, golang.org/x/crypto, validator/v10, google/uuid, caarlos0/env/v11)
- internal/config/config.go: загружает конфиг из env через caarlos0/env, метод DSN()
- internal/errs/errors.go: типы ErrNotFound, ErrForbidden, ErrConflict, ErrValidation, ErrUnauthorized + AppError wrapper
- internal/model/model.go: все доменные структуры (User, Poll, Question, Session, Participant, Answer, BrainstormIdea, BrainstormVote, RefreshToken)
- internal/handler/routes.go: chi-роутер с GET /health → {"status":"ok"}
- internal/middleware/auth.go: заглушка (заполняется в TASK-005)
- internal/ws/hub.go: заглушка (заполняется в TASK-012)
- internal/repository/repository.go: заглушка
- internal/service/service.go: заглушка
- pkg/normalize/text.go: функция Text() — trim, lowercase, strip punctuation
- pkg/badwords/filter.go: Filter() с RWMutex-защищённым словарём, Load() для hot reload
- cmd/server/main.go: запуск chi-роутера через config.Load()

**Изменённые файлы:**
- backend/go.mod (обновлён)
- backend/cmd/server/main.go (обновлён)
- backend/internal/config/config.go (новый)
- backend/internal/errs/errors.go (новый)
- backend/internal/model/model.go (новый)
- backend/internal/handler/routes.go (новый)
- backend/internal/middleware/auth.go (новый)
- backend/internal/ws/hub.go (новый)
- backend/internal/repository/repository.go (новый)
- backend/internal/service/service.go (новый)
- backend/pkg/normalize/text.go (новый)
- backend/pkg/badwords/filter.go (новый)

**ВАЖНО для следующей итерации:** перед `go build` нужно выполнить `go mod tidy` в директории backend/ для загрузки зависимостей и обновления go.sum.

**Статус:** done

---

### 2025-12-01 11:30 — TASK-003: PostgreSQL-миграции и схема базы данных
**Что сделано:**
- backend/migrations/000001_create_users.up/down.sql: таблицы users (UUID PK, email UNIQUE, password_hash, name, timestamps) и refresh_tokens (с индексами)
- backend/migrations/000002_create_polls_questions.up/down.sql: таблицы polls (user_id FK, scoring_rule CHECK, question_order CHECK) и questions (poll_id FK каскадное удаление, JSONB options, time_limit CHECK 5-300, position, индексы)
- backend/migrations/000003_create_sessions_participants.up/down.sql: таблицы sessions (room_code UNIQUE, status CHECK, JSONB question_order, partial UNIQUE INDEX на poll_id WHERE status<>'finished') и participants (session_token UNIQUE UUID)
- backend/migrations/000004_create_answers_brainstorm.up/down.sql: таблицы answers (UNIQUE participant_id+question_id, JSONB answer, is_hidden), brainstorm_ideas (votes_count), brainstorm_votes (UNIQUE idea_id+participant_id) + trigger для автообновления votes_count
- backend/cmd/migrate/main.go: CLI-инструмент для up/down/version через golang-migrate

**Изменённые файлы:**
- backend/migrations/000001_create_users.up.sql (новый)
- backend/migrations/000001_create_users.down.sql (новый)
- backend/migrations/000002_create_polls_questions.up.sql (новый)
- backend/migrations/000002_create_polls_questions.down.sql (новый)
- backend/migrations/000003_create_sessions_participants.up.sql (новый)
- backend/migrations/000003_create_sessions_participants.down.sql (новый)
- backend/migrations/000004_create_answers_brainstorm.up.sql (новый)
- backend/migrations/000004_create_answers_brainstorm.down.sql (новый)
- backend/cmd/migrate/main.go (новый)
- tasks.json (TASK-003 status → done)

**ВАЖНО для следующей итерации:** перед `go build ./...` выполни `go mod tidy` в backend/ — go.sum пустой, зависимости не подтянуты. Migrate CLI: `go run ./cmd/migrate up`, путь к миграциям задаётся через env MIGRATIONS_PATH (по умолчанию "./migrations").

**Статус:** done

### 2026-01-20 14:00 — TASK-005: Backend аутентификация (JWT + bcrypt)
**Что сделано:**
- internal/repository/user_repo.go: UserRepository interface + PostgresUserRepo (CreateUser, GetUserByEmail, GetUserByID, CreateRefreshToken, GetRefreshToken, DeleteRefreshToken, DeleteExpiredRefreshTokens). Конфликт email → errs.ErrConflict через pq.Error code 23505.
- internal/service/auth_service.go: AuthService interface + authService (Register, Login, Refresh, Logout). bcrypt cost=12, JWT HS256, refresh token = opaque UUID хранится в БД, rotation при Refresh. Экспортированная ValidateAccessToken() для middleware.
- internal/handler/auth_handler.go: authHandler с handleRegister, handleLogin, handleRefresh, handleLogout. JSON decode + go-playground/validator (email format, password min=8, name max=100). httpOnly cookie для refresh_token с SameSiteStrict.
- internal/middleware/auth.go: Auth() middleware — JWT Bearer extraction + ValidateAccessToken → user_id в context. RateLimiter struct с per-IP sliding window (sync.Map + mutex), RateLimit() middleware → 429 с Retry-After: 60.
- internal/handler/routes.go: обновлён — NewRouter принимает RouterDeps с AuthService и конфигом. /api/auth/* защищён RateLimit(5 req/min). /api/polls, /api/rooms, /api/upload — protected group с Auth middleware.
- cmd/server/main.go: подключение к PostgreSQL через sqlx, инициализация UserRepository → AuthService → Router.

**Изменённые файлы:**
- backend/internal/repository/user_repo.go (новый)
- backend/internal/service/auth_service.go (новый)
- backend/internal/handler/auth_handler.go (новый)
- backend/internal/middleware/auth.go (обновлён)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-005 status → done)

**ВАЖНО для следующей итерации:** перед `go build` выполни `go mod tidy` в backend/ — go.sum пустой. Следующие доступные critical задачи: TASK-012 (WebSocket Hub, deps: TASK-003 ✓) или TASK-004 (Frontend React init, deps: TASK-001 ✓) или TASK-007 (CRUD polls, deps: TASK-005 ✓).

**Статус:** done

---

### 2026-02-10 16:00 — TASK-007: Backend CRUD опросов (polls) с авторизацией
**Что сделано:**
- internal/repository/poll_repo.go: PollRepository interface + PostgresImpl (Create, GetByID, ListByUser, Update, Delete)
- internal/service/poll_service.go: PollService interface + pollService (Create, Get, List, Update, Delete, Copy). Проверка owner через userID, Copy добавляет suffix "(Копия)".
- internal/handler/poll_handler.go: pollHandler с handleList, handleCreate, handleGet, handleUpdate, handleDelete, handleCopy. go-playground/validator для входных данных.
- internal/handler/routes.go: добавлен PollService в RouterDeps, зарегистрированы все 6 маршрутов /api/polls/*.
- cmd/server/main.go: инициализация pollRepo + pollSvc, передача в RouterDeps.

**Изменённые файлы:**
- backend/internal/repository/poll_repo.go (новый)
- backend/internal/service/poll_service.go (новый)
- backend/internal/handler/poll_handler.go (новый)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-007 status → done)

**Статус:** done

**Следующие доступные critical задачи:**
- TASK-004 (Frontend React init, deps: TASK-001 ✓)
- TASK-008 (CRUD questions, deps: TASK-007 ✓)
- TASK-012 (WebSocket Hub, deps: TASK-003 ✓)

---

### 2026-02-17 10:00 — TASK-008: Backend CRUD вопросов (questions) всех 6 типов
**Что сделано:**
- internal/model/model.go: добавлен QuestionOption struct (Text, IsCorrect, ImageURL) и OptionList ([]QuestionOption) с реализацией driver.Valuer + sql.Scanner для JSONB хранения в PostgreSQL
- internal/repository/question_repo.go: QuestionRepository interface + PostgresImpl (Create, GetByID, ListByPoll, Update, Delete, Reorder в транзакции, MaxPosition)
- internal/service/question_service.go: QuestionService interface + questionService. Проверка владельца опроса через pollRepo. validateOptions: choice-типы требуют ≥2 вариантов и ≥1 правильного. Auto-position через MaxPosition+1 при создании.
- internal/handler/question_handler.go: 5 обработчиков (list, create, update, delete, reorder). reorderRequest обёртывает массив ReorderRequest с validator dive.
- internal/handler/routes.go: добавлен QuestionService в RouterDeps, зарегистрированы маршруты /{pollId}/questions/* вложенно в /polls
- cmd/server/main.go: инициализация questionRepo + questionSvc, передача в RouterDeps

**Маршруты:**
- GET /api/polls/{pollId}/questions
- POST /api/polls/{pollId}/questions
- PUT /api/polls/{pollId}/questions/{id}
- DELETE /api/polls/{pollId}/questions/{id}
- PATCH /api/polls/{pollId}/questions/reorder

**Изменённые файлы:**
- backend/internal/model/model.go (обновлён)
- backend/internal/repository/question_repo.go (новый)
- backend/internal/service/question_service.go (новый)
- backend/internal/handler/question_handler.go (новый)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-008 status → done)

**Коммиты:** 73458c2, 5bc1367, 0f5a324
**Статус:** done

**Следующие доступные critical задачи:**
- TASK-012 (WebSocket Hub, deps: TASK-003 ✓)
- TASK-004 (Frontend React init, deps: TASK-001 ✓)

---

### 2026-03-09 11:00 — TASK-012: Backend WebSocket Hub — управление комнатами и соединениями
**Что сделано:**
- internal/ws/messages.go: все типизированные структуры для входящих и исходящих WS-сообщений: 17 outgoing типов (connected, participant_joined/left, question_start, timer_tick, question_end, results, leaderboard, answer_accepted, answer_count, wordcloud_update, brainstorm_*, session_end, answer_hidden, error) и 9 incoming типов. NewEnvelope() для сериализации.
- internal/ws/room.go: Room с RWMutex, хранит map[*Client]bool, текущее состояние (RoomState: waiting/active/showing_question/showing_results/finished), ActiveQuestion, stopTimer channel. Методы: AddClient, RemoveClient, Broadcast, BroadcastToParticipants, SendToOrganizer, SendToClient, State/SetState, SetCurrentQuestion, SetStopTimer/StopCurrentTimer.
- internal/ws/hub.go: Hub с map[roomCode]*Room + RWMutex. Методы: CreateRoom, GetRoom, RemoveRoom, Register, Unregister (авто-cleanup finished пустых комнат), Broadcast.
- internal/ws/client.go: Client с readPump (SetReadLimit=4KB, pong handler, rate limiting 10 msg/sec per IP) и writePump (ping каждые 30 сек). Геттеры/сеттеры для role, name, sessionToken, participantID, userID.
- internal/ws/handler.go: WS HTTP upgrade endpoint HandleRoom. Определяет роль по JWT (Authorization header или ?token=). Participant: session_token из query param или генерирует новый UUID. Dispatch incoming messages с авторизационными проверками. Hooks onJoin/onLeave/onMessage для интеграции с conduct service (TASK-017).
- internal/handler/routes.go: добавлен WSHandler *ws.Handler в RouterDeps, зарегистрирован GET /ws/room/{code}.
- cmd/server/main.go: создание Hub + WSHandler, передача в RouterDeps.

**Изменённые файлы:**
- backend/internal/ws/messages.go (новый)
- backend/internal/ws/room.go (новый)
- backend/internal/ws/hub.go (обновлён — полная реализация)
- backend/internal/ws/client.go (новый)
- backend/internal/ws/handler.go (новый)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-012 status → done)

**ВАЖНО для следующей итерации:** перед `go build` выполни `go mod tidy` в backend/. Следующая доступная critical задача: TASK-013 (Backend: создание и управление сессиями, deps: TASK-012 ✓ + TASK-007 ✓).

**Статус:** done

---

### 2026-03-09 17:00 — TASK-013: Backend: создание и управление сессиями (комнатами)
**Что сделано:**
- internal/repository/session_repo.go: SessionRepository interface + PostgresImpl (Create, GetByCode, GetActiveByPoll, UpdateStatus). GetActiveByPoll использует `status <> 'finished'` для partial-uniqueness проверки.
- internal/service/room_service.go: RoomService interface + roomService (CreateRoom, GetRoom, ChangeState). CreateRoom: проверяет владельца опроса, проверяет наличие активной комнаты (ErrConflict), генерирует случайный 6-значный код (crypto/rand, retry на 23505), инициализирует Room в Hub. ChangeState: проверяет владельца, обновляет status в БД и синхронизирует state в Hub (active/finished).
- internal/handler/room_handler.go: roomHandler с handleCreate, handleGet, handleChangeState. Корректная обработка errs.ErrConflict → 409.
- internal/handler/routes.go: добавлен RoomService в RouterDeps, зарегистрированы маршруты POST/GET/PATCH /api/rooms/*.
- cmd/server/main.go: инициализация sessionRepo + roomSvc, передача в RouterDeps.

**Маршруты:**
- POST /api/rooms (JWT required)
- GET /api/rooms/{code} (JWT required)
- PATCH /api/rooms/{code}/state (JWT required)

**Изменённые файлы:**
- backend/internal/repository/session_repo.go (новый)
- backend/internal/service/room_service.go (новый)
- backend/internal/handler/room_handler.go (новый)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-013 status → done)

**Статус:** done

**Следующие доступные critical задачи:**
- TASK-014 (Backend: подключение участников через WebSocket, deps: TASK-013 ✓)

---

### 2026-03-09 19:00 — TASK-014: Backend: подключение участников через WebSocket с переподключением
**Что сделано:**
- internal/repository/participant_repo.go: ParticipantRepository interface + PostgresImpl (Create, GetBySessionToken, ListBySession, UpdateLastSeen). GetBySessionToken используется для reconnect-логики.
- internal/service/participant_service.go: ParticipantService interface + participantService (OnJoin, OnLeave, ListParticipants). OnJoin: проверяет session_token для reconnect → если найден в БД для той же сессии — восстанавливает участника; иначе создаёт нового. Отправляет {type:'connected', data:{session_token, role}} клиенту. Уведомляет организатора {type:'participant_joined'}. OnLeave: обновляет last_seen, уведомляет организатора {type:'participant_left'}.
- internal/handler/participant_handler.go: GET /api/rooms/{code}/participants → список участников (id, name, joined_at, total_score).
- internal/ws/client.go: добавлен метод TrySend() для не-блокирующей отправки сообщения клиенту.
- internal/handler/routes.go: добавлен ParticipantService в RouterDeps; WS join/leave hooks подключены к participantSvc; зарегистрирован GET /api/rooms/{code}/participants.
- cmd/server/main.go: инициализация participantRepo + participantSvc, передача в RouterDeps.

**Маршруты:**
- GET /api/rooms/{code}/participants (JWT required)
- WS join/leave → автоматически через hooks из participantSvc

**Изменённые файлы:**
- backend/internal/repository/participant_repo.go (новый)
- backend/internal/service/participant_service.go (новый)
- backend/internal/handler/participant_handler.go (новый)
- backend/internal/ws/client.go (добавлен TrySend)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-014 status → done)

**Статус:** done

**Следующие доступные задачи:**
- TASK-017 (critical): Backend: проведение опроса — показ вопроса и серверный таймер (deps: TASK-013 ✓)

---

### 2026-03-09 14:00 — TASK-017: Backend: проведение опроса — показ вопроса и серверный таймер
**Что сделано:**
- internal/ws/room.go: добавлен метод `TryFinishQuestion(questionID)` — атомарно проверяет, что данный вопрос ещё активен, очищает currentQuestion и переводит room.state в `showing_results`. Предотвращает двойную отправку question_end.
- internal/repository/answer_repo.go: AnswerRepository interface + PostgresImpl (Create, GetByParticipantAndQuestion, ListByQuestion, GetLeaderboard, UpdateParticipantScore). LeaderboardRow struct. Используется ConductService для вычисления результатов.
- internal/service/conduct_service.go: ConductService interface + conductService. `HandleMessage` диспетчеризует WS-сообщения `show_question` и `end_question`. `handleShowQuestion`: валидация вопроса принадлежности к сессии, broadcast `question_start` с options без `is_correct`, запуск таймер-горутины. `startTimer`: тикает каждую секунду, broadcast `timer_tick`, при достижении 0 вызывает `finishQuestion`. `finishQuestion`: TryFinishQuestion атомарно, broadcast `question_end` с revealed options, пустые `results`+`leaderboard` (заполнятся в TASK-018/019). `EndQuestion`: HTTP-вызов для досрочного завершения (проверяет владельца сессии). `buildParticipantOptions`: маскирует `is_correct` от участников при вопросе.
- internal/handler/room_handler.go: newRoomHandler принимает ConductService; handleChangeState обрабатывает action=`end_question` через conductSvc.EndQuestion.
- internal/handler/routes.go: ConductService добавлен в RouterDeps; wsHandler.SetMessageHandler(conductSvc.HandleMessage) устанавливает обработчик WS-сообщений.
- cmd/server/main.go: инициализация answerRepo + conductSvc, передача в RouterDeps.

**Маршруты/сообщения:**
- WS: `show_question` (organizer → server) → broadcast `question_start` + `timer_tick` × N + `question_end` + `results` + `leaderboard`
- WS: `end_question` (organizer → server) → досрочное завершение
- PATCH /api/rooms/{code}/state {action:"end_question"} → HTTP досрочное завершение

**Изменённые файлы:**
- backend/internal/ws/room.go (добавлен TryFinishQuestion)
- backend/internal/repository/answer_repo.go (новый)
- backend/internal/service/conduct_service.go (новый)
- backend/internal/handler/room_handler.go (обновлён)
- backend/internal/handler/routes.go (обновлён)
- backend/cmd/server/main.go (обновлён)
- tasks.json (TASK-017 status → done)

**Статус:** done

**Следующие доступные critical задачи:**
- TASK-018 (Backend: приём и валидация ответов, deps: TASK-017 ✓)

---

### 2026-03-09 21:00 — TASK-018: Backend: приём и валидация ответов участников
**Что сделано:**
- internal/ws/room.go: добавлены поле `answerCount int` и методы `ResetAnswerCount()`, `IncrementAnswerCount() int`, `AnswerCount() int` для in-memory подсчёта ответов на текущий вопрос.
- internal/service/conduct_service.go:
  - `HandleMessage` расширен: добавлены case для `submit_answer` и `submit_text`.
  - `handleShowQuestion`: вызывает `room.ResetAnswerCount()` при старте нового вопроса.
  - `handleSubmitAnswer`: валидирует роль участника, наличие активного вопроса, совпадение question_id, отсутствие дубликата (через `GetByParticipantAndQuestion`). Вычисляет `is_correct` через `computeIsCorrect`. Сохраняет ответ с серверным timestamp. Отправляет `answer_accepted` участнику, `answer_count` организатору.
  - `handleSubmitText`: то же для текстовых ответов (open_text/word_cloud), `is_correct=nil`.
  - `computeIsCorrect`: вычисляет правильность для single_choice (по индексу), image_choice (по индексу), multiple_choice (set equality). Для остальных типов — nil.

**Логика валидации "вопрос активен":** `room.CurrentQuestion() == nil` → вопрос завершён (timer expired или ранняя остановка через TryFinishQuestion). Timestamp ответа — `time.Now()` на сервере, response_time_ms вычисляется от `current.StartedAt`.

**Изменённые файлы:**
- backend/internal/ws/room.go (добавлены answerCount + методы)
- backend/internal/service/conduct_service.go (handleSubmitAnswer, handleSubmitText, computeIsCorrect)
- tasks.json (TASK-018 status → done)

**Статус:** done

**Следующие доступные задачи:**
- TASK-019 (high): Backend: система подсчёта баллов (deps: TASK-018 ✓)
- TASK-004 (critical): Frontend: React init (deps: TASK-001 ✓)

---

### 2026-03-09 23:00 — TASK-019: Backend: система подсчёта баллов (scoring) и лидерборд
**Что сделано:**
- pkg/scoring/scoring.go: функция `CalculateScore(basePoints, timeLimitSec, responseTimeMs int, scoringRule string, isCorrect *bool) int`. Три режима: `none` (всегда 0), `correct_answer` (basePoints или 0), `speed_bonus` (пропорционально оставшемуся времени, минимум 10% от basePoints).
- internal/ws/room.go: добавлены поля `ScoringRule string` и `Points int` в `ActiveQuestion`. Добавлен метод `ForEachParticipant(func(*Client))` для итерации участников без блокировки.
- internal/ws/messages.go: `AnswerAcceptedData` дополнен полем `IsCorrect *bool`. Добавлен `SessionEndData` с `Rankings`, `MyRank`, `MyScore`.
- internal/service/conduct_service.go: `handleShowQuestion` теперь загружает poll для получения `ScoringRule` и сохраняет его в `ActiveQuestion`. `handleSubmitAnswer` вычисляет реальный score через `scoring.CalculateScore`, обновляет `participants.total_score` через `answerRepo.UpdateParticipantScore`, отправляет score и is_correct в `answer_accepted`. `finishQuestion` отправляет персональный лидерборд (MyRank, MyScore) каждому участнику и общий — организатору (топ-5). Добавлен метод `EndSession` — завершает сессию, обновляет DB, broadcast `session_end` с финальным лидербордом (топ-10 + персональные данные участника).
- internal/repository/answer_repo.go: `GetLeaderboard` переписан с JOIN на `answers` для корректного тайбрейкинга по суммарному `response_time_ms` (быстрее ответивший — выше при равных очках).
- internal/handler/room_handler.go: action="end" теперь маршрутизируется через `conductSvc.EndSession` (с broadcast session_end), а не только через roomSvc.ChangeState.

**Изменённые файлы:**
- backend/pkg/scoring/scoring.go (новый)
- backend/internal/ws/room.go (ActiveQuestion расширен, ForEachParticipant)
- backend/internal/ws/messages.go (AnswerAcceptedData + SessionEndData)
- backend/internal/service/conduct_service.go (scoring, EndSession, personalized leaderboard)
- backend/internal/repository/answer_repo.go (тайбрейкинг по response_time_ms)
- backend/internal/handler/room_handler.go (action="end" → EndSession)
- tasks.json (TASK-019 status → done)

**Статус:** done

**Следующие доступные задачи:**
- TASK-004 (critical): Frontend React init (deps: TASK-001 ✓)
- TASK-028 (medium): Backend: история сессий (deps: TASK-019 ✓)
- TASK-011 (high): Backend: загрузка изображений (deps: TASK-005 ✓)

---

<!-- Агенты записывают сюда свои summary по формату:
### YYYY-MM-DD HH:MM — TASK-XXX: [название задачи]
**Что сделано:** ...
**Изменённые файлы:** ...
**Коммит:** ...
**Статус:** done / blocked
**Блокер (если есть):** ...
-->

---

*Файл создан автоматически при инициализации tasks.json*
