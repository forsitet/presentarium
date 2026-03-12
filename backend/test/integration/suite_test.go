//go:build integration

// Package integration_test provides isolated integration tests for the
// Presentarium backend. Each test gets a fresh in-process HTTP/WS server and
// calls truncateAll() to reset all DB state before running.
//
// The package requires a running PostgreSQL database. By default it connects to
// localhost; override with the TEST_DB_URL environment variable:
//
//	TEST_DB_URL=postgres://user:pass@host:5432/dbname?sslmode=disable \
//	  go test -tags=integration -v ./test/integration/
//
// Migrations are applied automatically via golang-migrate before any test runs.
package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"presentarium/internal/handler"
	"presentarium/internal/repository"
	"presentarium/internal/service"
	"presentarium/internal/ws"
)

// ─── Package-level DB shared across all tests ─────────────────────────────────

// globalDB is connected once in TestMain and reused by all tests.
// Each test creates its own testServer (fresh Hub + services) for isolation.
var globalDB *sqlx.DB

const testJWTSecret = "integration-test-jwt-secret"

// TestMain runs once for the package:
//  1. Connects to the test database (TEST_DB_URL or localhost default)
//  2. Applies all migrations automatically via golang-migrate
//  3. Runs all tests; exits with the test exit code
func TestMain(m *testing.M) {
	dbURL := os.Getenv("TEST_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/presentarium_test?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: skip — cannot connect to DB: %v\n", err)
		// Exit 0 so 'go test -tags=integration ./...' does not fail in CI
		// environments where PostgreSQL is not available.
		os.Exit(0)
	}

	// Apply migrations automatically. The test binary is run from the package
	// directory, so ../../migrations points to backend/migrations/.
	mig, err := migrate.New("file://../../migrations", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: migrate.New: %v\n", err)
		db.Close()
		os.Exit(1)
	}
	if err := mig.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintf(os.Stderr, "integration: migrate up: %v\n", err)
		db.Close()
		os.Exit(1)
	}
	srcErr, dbErr := mig.Close()
	if srcErr != nil || dbErr != nil {
		fmt.Fprintf(os.Stderr, "integration: migrate.Close: src=%v db=%v\n", srcErr, dbErr)
	}

	globalDB = db

	code := m.Run()

	db.Close()
	os.Exit(code)
}

// ─── Per-test server ──────────────────────────────────────────────────────────

// testServer wraps an in-process httptest.Server wired to a real PostgreSQL
// database. A fresh instance is created per test so that the in-memory WebSocket
// Hub and all service state is fully isolated between tests.
type testServer struct {
	server    *httptest.Server
	db        *sqlx.DB
	jwtSecret string
}

// buildTestServer creates a fully-wired in-process HTTP/WS server backed by
// the shared PostgreSQL database. Call it at the start of each test after
// truncateAll.
func buildTestServer(t *testing.T) *testServer {
	t.Helper()

	db := globalDB
	userRepo := repository.NewPostgresUserRepo(db)
	pollRepo := repository.NewPostgresPollRepo(db)
	questionRepo := repository.NewPostgresQuestionRepo(db)
	sessionRepo := repository.NewPostgresSessionRepo(db)
	participantRepo := repository.NewPostgresParticipantRepo(db)
	answerRepo := repository.NewPostgresAnswerRepo(db)
	brainstormRepo := repository.NewPostgresBrainstormRepo(db)

	authSvc := service.NewAuthService(userRepo, testJWTSecret, 60, 7, nil)
	pollSvc := service.NewPollService(pollRepo)
	questionSvc := service.NewQuestionService(questionRepo, pollRepo)

	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, testJWTSecret)

	roomSvc := service.NewRoomService(sessionRepo, pollRepo, questionRepo, hub)
	participantSvc := service.NewParticipantService(participantRepo, sessionRepo, hub)
	conductSvc := service.NewConductService(questionRepo, sessionRepo, pollRepo, answerRepo, brainstormRepo, hub)
	historySvc := service.NewHistoryService(sessionRepo, answerRepo, participantRepo, questionRepo)
	moderationSvc := service.NewModerationService(sessionRepo, answerRepo, brainstormRepo, hub)

	router := handler.NewRouter(handler.RouterDeps{
		AuthService:         authSvc,
		PollService:         pollSvc,
		QuestionService:     questionSvc,
		RoomService:         roomSvc,
		ParticipantService:  participantSvc,
		ConductService:      conductSvc,
		HistoryService:      historySvc,
		ModerationService:   moderationSvc,
		WSHandler:           wsHandler,
		JWTSecret:           testJWTSecret,
		RefreshTokenTTLDays: 7,
		UploadsDir:          t.TempDir(),
		CORSAllowedOrigin:   "*",
		AppBaseURL:          "http://localhost:5173",
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return &testServer{server: srv, db: db, jwtSecret: testJWTSecret}
}

// truncateAll deletes all rows from every application table in dependency order.
// Call this at the start of each test to guarantee a clean state.
func truncateAll(t *testing.T) {
	t.Helper()
	_, err := globalDB.Exec(`
		TRUNCATE TABLE
			brainstorm_votes,
			brainstorm_ideas,
			answers,
			participants,
			sessions,
			questions,
			polls,
			refresh_tokens,
			users
		CASCADE
	`)
	if err != nil {
		t.Fatalf("truncateAll: %v", err)
	}
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (ts *testServer) baseURL() string { return ts.server.URL }
func (ts *testServer) wsURL() string {
	return "ws" + strings.TrimPrefix(ts.server.URL, "http")
}

// doJSON sends a JSON request and returns (statusCode, decoded body map).
func (ts *testServer) doJSON(
	t *testing.T, method, path string, body interface{}, token string,
) (int, map[string]interface{}) {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("doJSON encode: %v", err)
		}
	}

	req, err := http.NewRequest(method, ts.baseURL()+path, &buf)
	if err != nil {
		t.Fatalf("doJSON new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doJSON %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// doRaw sends a request without a body and returns the raw response bytes.
func (ts *testServer) doRaw(t *testing.T, method, path, token string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, ts.baseURL()+path, nil)
	if err != nil {
		t.Fatalf("doRaw new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doRaw %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// getString extracts a string value from a decoded JSON map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// ─── WebSocket helpers ────────────────────────────────────────────────────────

// wsClient wraps a gorilla WebSocket connection with a buffered message channel
// for easy synchronous waiting in tests.
type wsClient struct {
	conn         *websocket.Conn
	sessionToken string
	msgs         chan map[string]interface{}
	done         chan struct{}
}

// dialParticipant connects to a room as a named participant and waits for the
// "connected" handshake message. The returned wsClient's sessionToken is set.
func dialParticipant(t *testing.T, wsBase, roomCode, name string) *wsClient {
	t.Helper()
	u := fmt.Sprintf("%s/ws/room/%s?name=%s", wsBase, roomCode, url.QueryEscape(name))
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dialParticipant %q: %v", name, err)
	}
	c := startWSClient(conn)
	msg := c.waitFor(t, "connected", 5*time.Second)
	if data, ok := msg["data"].(map[string]interface{}); ok {
		if st, ok := data["session_token"].(string); ok {
			c.sessionToken = st
		}
	}
	return c
}

// dialOrganizer connects to a room as the organizer using a JWT token.
func dialOrganizer(t *testing.T, wsBase, roomCode, accessToken string) *wsClient {
	t.Helper()
	u := fmt.Sprintf("%s/ws/room/%s?token=%s", wsBase, roomCode, accessToken)
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dialOrganizer: %v", err)
	}
	c := startWSClient(conn)
	c.waitFor(t, "connected", 5*time.Second)
	return c
}

// startWSClient starts the read goroutine and returns a wsClient.
func startWSClient(conn *websocket.Conn) *wsClient {
	c := &wsClient{
		conn: conn,
		msgs: make(chan map[string]interface{}, 256),
		done: make(chan struct{}),
	}
	go func() {
		defer close(c.done)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]interface{}
			if json.Unmarshal(raw, &m) == nil {
				select {
				case c.msgs <- m:
				default: // drop to avoid blocking on full buffer
				}
			}
		}
	}()
	return c
}

// send marshals and transmits a typed WS message.
func (c *wsClient) send(t *testing.T, msgType string, data interface{}) {
	t.Helper()
	b, _ := json.Marshal(map[string]interface{}{"type": msgType, "data": data})
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("wsClient.send %s: %v", msgType, err)
	}
}

// waitFor blocks until a message of the expected type is received, skipping
// unrelated messages. Fails the test if the timeout elapses first.
func (c *wsClient) waitFor(t *testing.T, msgType string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-c.msgs:
			if msg["type"] == msgType {
				return msg
			}
		case <-deadline:
			t.Fatalf("timeout waiting for WS message %q", msgType)
		}
	}
}

// close gracefully shuts the WebSocket connection.
func (c *wsClient) close() {
	_ = c.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	c.conn.Close()
}
