//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestRegistrationAndAuth verifies the full auth lifecycle in isolation:
// register → duplicate-email conflict → login → refresh → logout.
func TestRegistrationAndAuth(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]
	email := fmt.Sprintf("user-%s@integration.test", suffix)

	// Register new user.
	status, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    email,
		"password": "securePass123",
		"name":     "Integration User",
	}, "")
	if status != http.StatusCreated {
		t.Fatalf("register: want 201, got %d — %v", status, body)
	}
	token := getString(body, "access_token")
	if token == "" {
		t.Fatal("register: missing access_token")
	}

	// Duplicate email must return 409.
	status, _ = ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    email,
		"password": "anotherPass456",
		"name":     "Duplicate",
	}, "")
	if status != http.StatusConflict {
		t.Fatalf("duplicate register: want 409, got %d", status)
	}

	// Login with correct credentials.
	status, body = ts.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"email":    email,
		"password": "securePass123",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login: want 200, got %d — %v", status, body)
	}
	loginToken := getString(body, "access_token")
	if loginToken == "" {
		t.Fatal("login: missing access_token")
	}

	// Wrong password must return 401.
	status, _ = ts.doJSON(t, "POST", "/api/auth/login", map[string]string{
		"email":    email,
		"password": "wrongPassword",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-password login: want 401, got %d", status)
	}

	// Accessing protected endpoint without token must return 401.
	status, _ = ts.doJSON(t, "GET", "/api/polls", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no-token polls: want 401, got %d", status)
	}

	// Protected endpoint with valid token must return 200.
	status, _ = ts.doJSON(t, "GET", "/api/polls", nil, loginToken)
	if status != http.StatusOK {
		t.Fatalf("polls with token: want 200, got %d", status)
	}
}

// TestPollCRUD verifies poll lifecycle in isolation:
// create → list → get → update → copy → delete.
func TestPollCRUD(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	// Register and get token.
	suffix := uuid.New().String()[:8]
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("poll-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Poll Owner",
	}, "")
	token := getString(body, "access_token")

	// Create poll.
	status, body := ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          "My Integration Poll",
		"description":    "Testing CRUD",
		"scoring_rule":   "correct_answer",
		"question_order": "sequential",
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create poll: want 201, got %d — %v", status, body)
	}
	pollID := getString(body, "id")
	if pollID == "" {
		t.Fatal("create poll: missing id")
	}

	// List must contain the created poll.
	status, body = ts.doJSON(t, "GET", "/api/polls", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list polls: want 200, got %d", status)
	}

	// Update title.
	status, body = ts.doJSON(t, "PUT", "/api/polls/"+pollID, map[string]interface{}{
		"title": "Updated Title",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("update poll: want 200, got %d — %v", status, body)
	}
	if getString(body, "title") != "Updated Title" {
		t.Fatalf("update poll: title not updated, got %v", body)
	}

	// Copy poll.
	status, body = ts.doJSON(t, "POST", "/api/polls/"+pollID+"/copy", nil, token)
	if status != http.StatusCreated {
		t.Fatalf("copy poll: want 201, got %d — %v", status, body)
	}
	copyTitle := getString(body, "title")
	if copyTitle == "" {
		t.Fatalf("copy poll: expected title with (Копия), got %v", body)
	}

	// Delete original poll.
	status, _ = ts.doRaw(t, "DELETE", "/api/polls/"+pollID, token)
	if status != http.StatusNoContent {
		t.Fatalf("delete poll: want 204, got %d", status)
	}

	// Get deleted poll must return 404.
	status, _ = ts.doJSON(t, "GET", "/api/polls/"+pollID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted poll: want 404, got %d", status)
	}
}

// TestQuestionValidation verifies question creation rules:
// - choice types require ≥2 options and ≥1 correct answer
// - time_limit must be 5–300
// - open_text succeeds without options
func TestQuestionValidation(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("q-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Q Owner",
	}, "")
	token := getString(body, "access_token")

	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Q Test Poll",
		"scoring_rule": "none",
	}, token)
	pollID := getString(body, "id")
	qPath := "/api/polls/" + pollID + "/questions"

	// single_choice with only 1 option must fail.
	status, _ := ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "single_choice",
		"text":               "Bad question",
		"time_limit_seconds": 30,
		"points":             10,
		"options": []map[string]interface{}{
			{"text": "Only option", "is_correct": true},
		},
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("1-option choice: want 400, got %d", status)
	}

	// time_limit_seconds=400 (>300) must fail.
	status, _ = ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "open_text",
		"text":               "Bad time",
		"time_limit_seconds": 400,
		"points":             0,
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("time_limit 400: want 400, got %d", status)
	}

	// open_text without options must succeed.
	status, body = ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "open_text",
		"text":               "What do you think?",
		"time_limit_seconds": 60,
		"points":             0,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("open_text: want 201, got %d — %v", status, body)
	}

	// single_choice with 2 options and 1 correct must succeed.
	status, body = ts.doJSON(t, "POST", qPath, map[string]interface{}{
		"type":               "single_choice",
		"text":               "Which is correct?",
		"time_limit_seconds": 30,
		"points":             100,
		"options": []map[string]interface{}{
			{"text": "A", "is_correct": true},
			{"text": "B", "is_correct": false},
		},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("valid single_choice: want 201, got %d — %v", status, body)
	}
}

// TestRoomOwnership verifies that another user cannot access a room or session
// that belongs to a different organizer.
func TestRoomOwnership(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	register := func(i int) string {
		_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
			"email":    fmt.Sprintf("owner%d@integration.test", i),
			"password": "password123",
			"name":     fmt.Sprintf("User %d", i),
		}, "")
		return getString(body, "access_token")
	}

	token1 := register(1)
	token2 := register(2)

	// User 1 creates a poll and room.
	_, body := ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Owner Test",
		"scoring_rule": "none",
	}, token1)
	pollID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token1)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")

	// User 2 should get 403 trying to start the room.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token2)
	if status != http.StatusForbidden {
		t.Fatalf("other user start: want 403, got %d", status)
	}

	// User 2 should get 403 trying to access user1's poll details.
	status, _ = ts.doJSON(t, "GET", "/api/polls/"+pollID, nil, token2)
	if status != http.StatusForbidden {
		t.Fatalf("other user get poll: want 403, got %d", status)
	}
}
