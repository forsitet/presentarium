//go:build integration

// Package e2e_test contains end-to-end integration tests that require a
// running PostgreSQL database. The DB URL is read from TEST_DB_URL (defaults
// to postgres://postgres:postgres@localhost:5432/presentarium_test?sslmode=disable).
//
// Run with:
//
//	go test -tags=integration -v ./test/e2e/
//
// The database must already have migrations applied. Each test generates
// unique data using a UUID suffix so parallel runs do not conflict.
package e2e_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"presentarium/internal/handler"
	"presentarium/internal/repository"
	"presentarium/internal/service"
	"presentarium/internal/ws"
)

// testServer holds the in-process HTTP+WS server and its DB handle.
type testServer struct {
	server    *httptest.Server
	db        *sqlx.DB
	jwtSecret string
}

// setupTestServer creates a fully-wired HTTP test server backed by a real
// PostgreSQL database. The database must already have migrations applied.
func setupTestServer(t *testing.T) *testServer {
	t.Helper()

	dbURL := os.Getenv("TEST_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/presentarium_test?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to database: %v", err)
	}

	// Quick sanity check that migrations have been applied.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		db.Close()
		t.Skipf("skipping: DB not migrated (no 'users' table): %v", err)
	}

	jwtSecret := "test-jwt-secret-e2e"

	userRepo := repository.NewPostgresUserRepo(db)
	pollRepo := repository.NewPostgresPollRepo(db)
	questionRepo := repository.NewPostgresQuestionRepo(db)
	sessionRepo := repository.NewPostgresSessionRepo(db)
	participantRepo := repository.NewPostgresParticipantRepo(db)
	answerRepo := repository.NewPostgresAnswerRepo(db)
	brainstormRepo := repository.NewPostgresBrainstormRepo(db)

	authSvc := service.NewAuthService(userRepo, jwtSecret, 60, 7, nil)
	pollSvc := service.NewPollService(pollRepo)
	questionSvc := service.NewQuestionService(questionRepo, pollRepo)

	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, jwtSecret)

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
		JWTSecret:           jwtSecret,
		RefreshTokenTTLDays: 7,
		UploadsDir:          t.TempDir(),
		CORSAllowedOrigin:   "*",
		AppBaseURL:          "http://localhost:5173",
	})

	srv := httptest.NewServer(router)

	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})

	return &testServer{server: srv, db: db, jwtSecret: jwtSecret}
}

// baseURL returns the HTTP base URL of the test server.
func (ts *testServer) baseURL() string { return ts.server.URL }

// wsURL converts the HTTP server URL to a WebSocket URL.
func (ts *testServer) wsURL() string {
	return "ws" + strings.TrimPrefix(ts.server.URL, "http")
}

// doJSON performs an HTTP request with a JSON body and decodes the response.
func (ts *testServer) doJSON(
	t *testing.T, method, path string, body interface{}, token string,
) (int, map[string]interface{}) {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	req, err := http.NewRequest(method, ts.baseURL()+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// doRaw performs an HTTP request and returns the raw response body.
func (ts *testServer) doRaw(t *testing.T, method, path, token string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, ts.baseURL()+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// wsClient wraps a WebSocket connection with message routing helpers.
type wsClient struct {
	conn         *websocket.Conn
	sessionToken string
	msgs         chan map[string]interface{}
	done         chan struct{}
}

// connectParticipant dials the room as a named participant and waits for "connected".
func connectParticipant(t *testing.T, wsBase, roomCode, name string) *wsClient {
	t.Helper()

	u := fmt.Sprintf("%s/ws/room/%s?name=%s", wsBase, roomCode, url.QueryEscape(name))
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("ws dial participant %q: %v", name, err)
	}
	c := newWSClient(conn)

	msg := c.waitForType(t, "connected", 5*time.Second)
	if data, ok := msg["data"].(map[string]interface{}); ok {
		if st, ok := data["session_token"].(string); ok {
			c.sessionToken = st
		}
	}
	return c
}

// connectOrganizer dials the room as the organizer using a JWT access token.
func connectOrganizer(t *testing.T, wsBase, roomCode, accessToken string) *wsClient {
	t.Helper()

	u := fmt.Sprintf("%s/ws/room/%s?token=%s", wsBase, roomCode, accessToken)
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("ws dial organizer: %v", err)
	}
	c := newWSClient(conn)
	c.waitForType(t, "connected", 5*time.Second)
	return c
}

func newWSClient(conn *websocket.Conn) *wsClient {
	c := &wsClient{
		conn: conn,
		msgs: make(chan map[string]interface{}, 128),
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
				default: // drop if buffer full to avoid blocking
				}
			}
		}
	}()
	return c
}

// send marshals and sends a typed WS message.
func (c *wsClient) send(t *testing.T, msgType string, data interface{}) {
	t.Helper()
	b, err := json.Marshal(map[string]interface{}{"type": msgType, "data": data})
	if err != nil {
		t.Fatalf("marshal ws message: %v", err)
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("ws write %s: %v", msgType, err)
	}
}

// waitForType blocks until a message of the given type arrives, discarding
// unrelated messages. Fails the test after timeout.
func (c *wsClient) waitForType(t *testing.T, msgType string, timeout time.Duration) map[string]interface{} {
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

// close gracefully shuts the WS connection.
func (c *wsClient) close() {
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.conn.Close()
}

// countCSVDataRows counts non-empty lines excluding the BOM and header row.
func countCSVDataRows(raw []byte) int {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	n := 0
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			n++
		}
	}
	if n <= 1 {
		return 0
	}
	return n - 1
}

func extractString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestFullQuizScenario is the canonical E2E acceptance test covering:
//
//	register → create poll → add 2 questions (single_choice + open_text) →
//	launch room → 3 participants join → run 2 questions → end session →
//	verify leaderboard → export CSV with 6 data rows.
func TestFullQuizScenario(t *testing.T) {
	ts := setupTestServer(t)
	suffix := uuid.New().String()[:8]

	// ── 1. Register organizer ─────────────────────────────────────────────────
	status, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("organizer-%s@e2e.test", suffix),
		"password": "password123",
		"name":     "E2E Organizer",
	}, "")
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d — %v", status, body)
	}
	token := extractString(body, "access_token")
	if token == "" {
		t.Fatal("register: missing access_token")
	}
	t.Logf("✓ organizer registered")

	// ── 2. Create poll with correct_answer scoring ────────────────────────────
	status, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          "E2E Quiz",
		"scoring_rule":   "correct_answer",
		"question_order": "sequential",
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create poll: expected 201, got %d — %v", status, body)
	}
	pollID := extractString(body, "id")
	t.Logf("✓ poll created: %s", pollID)

	// ── 3. Add question 1 (single_choice, option index 1 correct) ────────────
	qPath := "/api/polls/" + pollID + "/questions"
	status, body = ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "single_choice",
		"text":               "What is 2+2?",
		"time_limit_seconds": 30,
		"points":             100,
		"options": []map[string]interface{}{
			{"text": "3", "is_correct": false},
			{"text": "4", "is_correct": true},
		},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create q1: expected 201, got %d — %v", status, body)
	}
	q1ID := extractString(body, "id")
	t.Logf("✓ question 1 (single_choice): %s", q1ID)

	// ── 4. Add question 2 (open_text) ─────────────────────────────────────────
	status, body = ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "open_text",
		"text":               "What is your favourite language?",
		"time_limit_seconds": 30,
		"points":             0,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create q2: expected 201, got %d — %v", status, body)
	}
	q2ID := extractString(body, "id")
	t.Logf("✓ question 2 (open_text): %s", q2ID)

	// ── 5. Create room ────────────────────────────────────────────────────────
	status, body = ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: expected 201, got %d — %v", status, body)
	}
	roomCode := extractString(body, "room_code")
	sessionID := extractString(body, "session_id")
	if roomCode == "" || sessionID == "" {
		t.Fatalf("create room: missing room_code or session_id in %v", body)
	}
	t.Logf("✓ room created: code=%s session=%s", roomCode, sessionID)

	// ── 6. Connect 3 participants ─────────────────────────────────────────────
	names := []string{"Alice", "Bob", "Charlie"}
	parts := make([]*wsClient, len(names))
	for i, name := range names {
		parts[i] = connectParticipant(t, ts.wsURL(), roomCode, name)
		t.Logf("✓ participant %q connected", name)
	}
	defer func() {
		for _, p := range parts {
			p.close()
		}
	}()

	// ── 7. Organizer joins via WS ─────────────────────────────────────────────
	org := connectOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()
	t.Logf("✓ organizer connected via WS")

	// ── 8. Start session ──────────────────────────────────────────────────────
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)
	if status != http.StatusOK {
		t.Fatalf("start: expected 200, got %d", status)
	}
	t.Logf("✓ session started")

	// ── 9. Question 1: all 3 participants answer correctly (index 1 = "4") ───
	org.send(t, "show_question", map[string]string{"question_id": q1ID})

	var wg sync.WaitGroup
	for _, p := range parts {
		wg.Add(1)
		go func(participant *wsClient) {
			defer wg.Done()
			participant.waitForType(t, "question_start", 5*time.Second)
			participant.send(t, "submit_answer", map[string]interface{}{
				"question_id": q1ID,
				"answer":      1, // "4" — the correct option
			})
			participant.waitForType(t, "answer_accepted", 5*time.Second)
		}(p)
	}
	wg.Wait()
	t.Logf("✓ all 3 participants answered question 1")

	// End question 1 early.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	if status != http.StatusOK {
		t.Fatalf("end_question 1: expected 200, got %d", status)
	}
	for _, p := range parts {
		p.waitForType(t, "question_end", 5*time.Second)
	}
	t.Logf("✓ question 1 ended")

	// ── 10. Question 2: all 3 participants submit text ────────────────────────
	org.send(t, "show_question", map[string]string{"question_id": q2ID})

	for _, p := range parts {
		wg.Add(1)
		go func(participant *wsClient) {
			defer wg.Done()
			participant.waitForType(t, "question_start", 5*time.Second)
			participant.send(t, "submit_text", map[string]interface{}{
				"question_id": q2ID,
				"text":        "Go",
			})
			participant.waitForType(t, "answer_accepted", 5*time.Second)
		}(p)
	}
	wg.Wait()
	t.Logf("✓ all 3 participants answered question 2")

	// End question 2 early.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	if status != http.StatusOK {
		t.Fatalf("end_question 2: expected 200, got %d", status)
	}
	for _, p := range parts {
		p.waitForType(t, "question_end", 5*time.Second)
	}
	t.Logf("✓ question 2 ended")

	// ── 11. End session ───────────────────────────────────────────────────────
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end"}, token)
	if status != http.StatusOK {
		t.Fatalf("end session: expected 200, got %d", status)
	}
	t.Logf("✓ session ended")

	// Give the server a moment to finalise DB writes.
	time.Sleep(100 * time.Millisecond)

	// ── 12. Verify session in history list ───────────────────────────────────
	statusH, rawSessions := ts.doRaw(t, "GET", "/api/sessions", token)
	if statusH != http.StatusOK {
		t.Fatalf("list sessions: expected 200, got %d", statusH)
	}
	var sessions []map[string]interface{}
	if err := json.Unmarshal(rawSessions, &sessions); err != nil {
		t.Fatalf("decode sessions list: %v — raw: %s", err, rawSessions)
	}
	if len(sessions) == 0 {
		t.Fatal("expected at least 1 session in history")
	}
	t.Logf("✓ session appears in history (%d sessions total)", len(sessions))

	// ── 13. Verify leaderboard — all 3 scored 100 pts ────────────────────────
	statusD, rawDetail := ts.doRaw(t, "GET", "/api/sessions/"+sessionID, token)
	if statusD != http.StatusOK {
		t.Fatalf("get session detail: expected 200, got %d — %s", statusD, rawDetail)
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(rawDetail, &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	if lb, ok := detail["leaderboard"].([]interface{}); ok {
		if len(lb) != 3 {
			t.Errorf("leaderboard: expected 3 entries, got %d", len(lb))
		}
		for _, entry := range lb {
			e, _ := entry.(map[string]interface{})
			score, _ := e["score"].(float64)
			if score != 100 {
				t.Errorf("participant %v: expected 100 pts, got %.0f", e["name"], score)
			}
		}
		t.Logf("✓ leaderboard: 3 entries, each 100 pts")
	}

	// ── 14. Export CSV — expect 6 data rows (3 participants × 2 questions) ───
	csvStatus, csvBody := ts.doRaw(t, "GET",
		"/api/sessions/"+sessionID+"/export/csv", token)
	if csvStatus != http.StatusOK {
		t.Fatalf("export csv: expected 200, got %d — %s", csvStatus, csvBody)
	}
	dataRows := countCSVDataRows(csvBody)
	if dataRows != 6 {
		t.Errorf("CSV: expected 6 data rows, got %d\n%s", dataRows, csvBody)
	} else {
		t.Logf("✓ CSV: 6 data rows (3 participants × 2 questions)")
	}

	// Verify required columns are present in the header.
	lines := strings.Split(strings.TrimSpace(string(csvBody)), "\n")
	if len(lines) > 0 {
		header := lines[0]
		for _, col := range []string{
			"participant_name", "question_text", "answer",
			"is_correct", "score", "response_time_ms",
		} {
			if !strings.Contains(header, col) {
				t.Errorf("CSV header missing column %q; header: %s", col, header)
			}
		}
		t.Logf("✓ CSV header has all required columns")
	}

	t.Log("✓ Full E2E quiz scenario passed")
}

// TestDuplicateActiveRoom verifies that creating a second active room for
// the same poll returns 409 Conflict.
func TestDuplicateActiveRoom(t *testing.T) {
	ts := setupTestServer(t)
	suffix := uuid.New().String()[:8]

	status, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("dup-%s@e2e.test", suffix),
		"password": "password123",
		"name":     "Dup Tester",
	}, "")
	if status != http.StatusCreated {
		t.Fatalf("register: %d %v", status, body)
	}
	token := extractString(body, "access_token")

	status, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          "Dup Test",
		"scoring_rule":   "none",
		"question_order": "sequential",
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create poll: %d %v", status, body)
	}
	pollID := extractString(body, "id")

	// First room — must succeed.
	status, _ = ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusCreated {
		t.Fatalf("first room: expected 201, got %d", status)
	}

	// Second room for same poll — must return 409.
	status, _ = ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusConflict {
		t.Errorf("second room: expected 409, got %d", status)
	}
	t.Log("✓ duplicate active room correctly rejected with 409")
}

// TestCorrectAnswerScoring verifies the correct_answer scoring rule:
// a participant who picks the correct option earns full points;
// one who picks wrong earns zero.
func TestCorrectAnswerScoring(t *testing.T) {
	ts := setupTestServer(t)
	suffix := uuid.New().String()[:8]

	status, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("scoring-%s@e2e.test", suffix),
		"password": "password123",
		"name":     "Scoring Tester",
	}, "")
	if status != http.StatusCreated {
		t.Fatalf("register: %d %v", status, body)
	}
	token := extractString(body, "access_token")

	status, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          "Scoring Test",
		"scoring_rule":   "correct_answer",
		"question_order": "sequential",
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create poll: %d %v", status, body)
	}
	pollID := extractString(body, "id")

	// Question: option 0 correct, points = 50.
	status, body = ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "Pick the winner",
			"time_limit_seconds": 30,
			"points":             50,
			"options": []map[string]interface{}{
				{"text": "Correct", "is_correct": true},
				{"text": "Wrong", "is_correct": false},
			},
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("create question: %d %v", status, body)
	}
	qID := extractString(body, "id")

	// Create room and start session.
	status, body = ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: %d %v", status, body)
	}
	roomCode := extractString(body, "room_code")
	sessionID := extractString(body, "session_id")

	pCorrect := connectParticipant(t, ts.wsURL(), roomCode, "Winner")
	pWrong := connectParticipant(t, ts.wsURL(), roomCode, "Loser")
	defer pCorrect.close()
	defer pWrong.close()

	org := connectOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)

	// Show question.
	org.send(t, "show_question", map[string]string{"question_id": qID})

	// Collect scores from answer_accepted messages.
	scores := make(map[string]int, 2)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, tc := range []struct {
		p      *wsClient
		name   string
		answer int
	}{
		{pCorrect, "Winner", 0},
		{pWrong, "Loser", 1},
	} {
		wg.Add(1)
		go func(p *wsClient, name string, ans int) {
			defer wg.Done()
			p.waitForType(t, "question_start", 5*time.Second)
			p.send(t, "submit_answer", map[string]interface{}{"question_id": qID, "answer": ans})
			msg := p.waitForType(t, "answer_accepted", 5*time.Second)
			if data, ok := msg["data"].(map[string]interface{}); ok {
				score := int(data["score"].(float64))
				mu.Lock()
				scores[name] = score
				mu.Unlock()
			}
		}(tc.p, tc.name, tc.answer)
	}
	wg.Wait()

	if scores["Winner"] != 50 {
		t.Errorf("correct answer: expected 50 pts, got %d", scores["Winner"])
	}
	if scores["Loser"] != 0 {
		t.Errorf("wrong answer: expected 0 pts, got %d", scores["Loser"])
	}
	t.Logf("✓ scoring: correct=%d, wrong=%d", scores["Winner"], scores["Loser"])

	// End question, end session.
	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	pCorrect.waitForType(t, "question_end", 5*time.Second)
	pWrong.waitForType(t, "question_end", 5*time.Second)

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end"}, token)

	time.Sleep(100 * time.Millisecond)

	// Verify leaderboard order via session detail.
	_, rawDetail := ts.doRaw(t, "GET", "/api/sessions/"+sessionID, token)
	var detail map[string]interface{}
	if err := json.Unmarshal(rawDetail, &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	if lb, ok := detail["leaderboard"].([]interface{}); ok && len(lb) > 0 {
		first := lb[0].(map[string]interface{})
		if name, _ := first["name"].(string); name != "Winner" {
			t.Errorf("leaderboard rank 1: expected 'Winner', got %q", name)
		} else {
			t.Log("✓ 'Winner' is rank 1 in leaderboard")
		}
	}
}
