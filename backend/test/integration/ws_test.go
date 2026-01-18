//go:build integration

package integration_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestWSParticipantJoin verifies participant WebSocket connect/disconnect flow:
//   - Participant connects → receives "connected" with session_token
//   - Organizer receives "participant_joined"
//   - Participant list endpoint reflects the new participant
func TestWSParticipantJoin(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("host-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Host",
	}, "")
	token := getString(body, "access_token")

	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "WS Join Test",
		"scoring_rule": "none",
	}, token)
	pollID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")

	// Connect organizer first so it can receive participant_joined.
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	// Connect participant.
	part := dialParticipant(t, ts.wsURL(), roomCode, "Alice")
	defer part.close()

	if part.sessionToken == "" {
		t.Fatal("participant: expected non-empty session_token")
	}

	// Organizer should receive participant_joined.
	org.waitFor(t, "participant_joined", 3*time.Second)

	// REST endpoint must list the participant.
	status, body = ts.doJSON(t, "GET", "/api/rooms/"+roomCode+"/participants", nil, token)
	if status != http.StatusOK {
		t.Fatalf("participants list: want 200, got %d", status)
	}
	items, _ := body["participants"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("participants list: want 1, got %d — %v", len(items), body)
	}
}

// TestWSQuestionFlow is the core WebSocket interaction test:
//
//	connect participant → question_start → submit_answer → answer_accepted →
//	question_end → results → leaderboard
//
// This satisfies TASK-040 acceptance criterion:
// "Тест WS: подключение, получение question_start, отправка submit_answer,
// получение answer_accepted".
func TestWSQuestionFlow(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]

	// Register organizer.
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("org-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Organizer",
	}, "")
	token := getString(body, "access_token")

	// Create poll + single_choice question.
	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          "WS Flow Poll",
		"scoring_rule":   "correct_answer",
		"question_order": "sequential",
	}, token)
	pollID := getString(body, "id")

	_, body = ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions", map[string]interface{}{
		"type":               "single_choice",
		"text":               "What is 1+1?",
		"time_limit_seconds": 30,
		"points":             100,
		"options": []map[string]interface{}{
			{"text": "1", "is_correct": false},
			{"text": "2", "is_correct": true},
		},
	}, token)
	questionID := getString(body, "id")
	if questionID == "" {
		t.Fatalf("create question: missing id — %v", body)
	}

	// Create room.
	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")

	// Connect participant and organizer via WebSocket.
	part := dialParticipant(t, ts.wsURL(), roomCode, "Bob")
	defer part.close()

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	// Start session.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)
	if status != http.StatusOK {
		t.Fatalf("start session: want 200, got %d", status)
	}

	// Organizer shows the question via WebSocket.
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	// ── Core assertion: participant receives question_start ───────────────────
	qStart := part.waitFor(t, "question_start", 5*time.Second)
	qData, _ := qStart["data"].(map[string]interface{})
	if qData["question_id"] != questionID {
		t.Fatalf("question_start: wrong question_id: %v", qData)
	}
	t.Logf("✓ participant received question_start for question %s", questionID)

	// ── Core assertion: participant submits answer ────────────────────────────
	part.send(t, "submit_answer", map[string]interface{}{
		"question_id": questionID,
		"answer":      1, // index 1 = "2" (correct)
	})

	// ── Core assertion: participant receives answer_accepted ──────────────────
	accepted := part.waitFor(t, "answer_accepted", 5*time.Second)
	t.Logf("✓ participant received answer_accepted: %v", accepted["data"])

	// Duplicate answer must NOT produce a second answer_accepted.
	part.send(t, "submit_answer", map[string]interface{}{
		"question_id": questionID,
		"answer":      0,
	})
	// Give the server time to process and potentially send an error message.
	time.Sleep(100 * time.Millisecond)

	// Drain messages — must not contain another answer_accepted.
	draining := true
	for draining {
		select {
		case msg := <-part.msgs:
			if msg["type"] == "answer_accepted" {
				t.Fatal("duplicate submit: received second answer_accepted, expected none")
			}
		default:
			draining = false
		}
	}
	t.Logf("✓ duplicate answer correctly rejected")

	// End question early via HTTP.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	if status != http.StatusOK {
		t.Fatalf("end_question: want 200, got %d", status)
	}

	// Participant must receive question_end.
	part.waitFor(t, "question_end", 5*time.Second)
	t.Logf("✓ participant received question_end")

	// Participant must receive leaderboard with 1 entry (Bob answered correctly).
	lb := part.waitFor(t, "leaderboard", 5*time.Second)
	lbData, _ := lb["data"].(map[string]interface{})
	rankings, _ := lbData["rankings"].([]interface{})
	if len(rankings) != 1 {
		t.Fatalf("leaderboard: want 1 entry, got %d — %v", len(rankings), lbData)
	}
	entry, _ := rankings[0].(map[string]interface{})
	if entry["name"] != "Bob" {
		t.Fatalf("leaderboard entry: want name=Bob, got %v", entry)
	}
	t.Logf("✓ leaderboard received with 1 entry: %v", entry)
}

// TestWSMultiParticipantScoring verifies that:
//   - All participants receive question_start simultaneously
//   - Scoring works correctly (correct vs wrong answer)
//   - Leaderboard after question is properly sorted
func TestWSMultiParticipantScoring(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]

	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("multi-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Host",
	}, "")
	token := getString(body, "access_token")

	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Multi Score Poll",
		"scoring_rule": "correct_answer",
	}, token)
	pollID := getString(body, "id")

	_, body = ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions", map[string]interface{}{
		"type":               "single_choice",
		"text":               "Capital of France?",
		"time_limit_seconds": 30,
		"points":             50,
		"options": []map[string]interface{}{
			{"text": "London", "is_correct": false},
			{"text": "Paris", "is_correct": true},
			{"text": "Berlin", "is_correct": false},
		},
	}, token)
	questionID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")

	// Connect 2 participants: Alice (correct) and Charlie (wrong).
	alice := dialParticipant(t, ts.wsURL(), roomCode, "Alice")
	defer alice.close()
	charlie := dialParticipant(t, ts.wsURL(), roomCode, "Charlie")
	defer charlie.close()

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)
	if status != http.StatusOK {
		t.Fatalf("start: want 200, got %d", status)
	}

	org.send(t, "show_question", map[string]string{"question_id": questionID})

	var wg sync.WaitGroup

	// Alice answers correctly (index 1 = Paris).
	wg.Add(1)
	go func() {
		defer wg.Done()
		alice.waitFor(t, "question_start", 5*time.Second)
		alice.send(t, "submit_answer", map[string]interface{}{
			"question_id": questionID,
			"answer":      1,
		})
		alice.waitFor(t, "answer_accepted", 5*time.Second)
	}()

	// Charlie answers wrongly (index 0 = London).
	wg.Add(1)
	go func() {
		defer wg.Done()
		charlie.waitFor(t, "question_start", 5*time.Second)
		charlie.send(t, "submit_answer", map[string]interface{}{
			"question_id": questionID,
			"answer":      0,
		})
		charlie.waitFor(t, "answer_accepted", 5*time.Second)
	}()

	wg.Wait()
	t.Logf("✓ both participants answered")

	// End question.
	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)

	alice.waitFor(t, "question_end", 5*time.Second)
	charlie.waitFor(t, "question_end", 5*time.Second)

	// Leaderboard: Alice (50 pts, correct) must be ranked first.
	lb := alice.waitFor(t, "leaderboard", 5*time.Second)
	lbData, _ := lb["data"].(map[string]interface{})
	rankings, _ := lbData["rankings"].([]interface{})
	if len(rankings) == 0 {
		t.Fatalf("leaderboard: want at least 1 entry, got 0 — %v", lbData)
	}
	first, _ := rankings[0].(map[string]interface{})
	if first["name"] != "Alice" {
		t.Fatalf("leaderboard rank 1: want Alice (correct answer), got %v", first["name"])
	}
	t.Logf("✓ leaderboard: Alice (correct) ranked first — %v", rankings)
}

// TestWSSessionEnd verifies the full session lifecycle from start to finish,
// ensuring all participants receive session_end.
func TestWSSessionEnd(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]

	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("end-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Host",
	}, "")
	token := getString(body, "access_token")

	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Session End Poll",
		"scoring_rule": "none",
	}, token)
	pollID := getString(body, "id")

	ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions", map[string]interface{}{
		"type":               "open_text",
		"text":               "How are you?",
		"time_limit_seconds": 30,
		"points":             0,
	}, token)

	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")

	part := dialParticipant(t, ts.wsURL(), roomCode, "Dana")
	defer part.close()

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)

	// End session directly without running any question.
	status, _ = ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end"}, token)
	if status != http.StatusOK {
		t.Fatalf("end session: want 200, got %d", status)
	}

	// Participant must receive session_end.
	part.waitFor(t, "session_end", 5*time.Second)
	t.Logf("✓ participant received session_end")

	// Session should appear in history.
	time.Sleep(50 * time.Millisecond)
	status, body = ts.doJSON(t, "GET", "/api/sessions", nil, token)
	if status != http.StatusOK {
		t.Fatalf("sessions history: want 200, got %d", status)
	}
	sessions, _ := body["sessions"].([]interface{})
	if len(sessions) == 0 {
		t.Fatalf("sessions history: expected at least 1 session, got 0 — %v", body)
	}
	t.Logf("✓ session appears in history")
}
