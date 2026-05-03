//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ─── #3 — Poll list returns only the caller's polls ────────────────────────────

// TestPollListIsolation verifies that GET /api/polls returns exactly the polls
// owned by the caller — even when another user has polls of their own. The
// existing TestRoomOwnership test only checks that user B gets 403 on user A's
// poll detail, which doesn't rule out leakage in the list endpoint.
func TestPollListIsolation(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	tokenA := registerOrganizer(t, ts, "ownerA")
	tokenB := registerOrganizer(t, ts, "ownerB")

	// User A creates 2 polls.
	for _, title := range []string{"A-1", "A-2"} {
		status, body := ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
			"title":        title,
			"scoring_rule": "none",
		}, tokenA)
		if status != http.StatusCreated {
			t.Fatalf("create poll %q for A: want 201, got %d — %v", title, status, body)
		}
	}
	// User B creates 1 poll.
	status, body := ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "B-1",
		"scoring_rule": "none",
	}, tokenB)
	if status != http.StatusCreated {
		t.Fatalf("create poll for B: want 201, got %d — %v", status, body)
	}

	// User A's list must contain exactly 2 polls, none of them "B-1".
	listA := listPolls(t, ts, tokenA)
	if len(listA) != 2 {
		t.Fatalf("user A list: want 2 polls, got %d — %v", len(listA), listA)
	}
	for _, p := range listA {
		title := getString(p, "title")
		if !strings.HasPrefix(title, "A-") {
			t.Errorf("user A list contains foreign poll %q", title)
		}
	}

	// User B's list must contain exactly 1 poll, its title "B-1".
	listB := listPolls(t, ts, tokenB)
	if len(listB) != 1 {
		t.Fatalf("user B list: want 1 poll, got %d — %v", len(listB), listB)
	}
	if got := getString(listB[0], "title"); got != "B-1" {
		t.Errorf("user B sole poll title = %q, want B-1", got)
	}
	t.Logf("✓ poll list correctly scoped per user")
}

// listPolls fetches /api/polls and decodes the response as a JSON array, since
// the testServer.doJSON helper always decodes into a map (which silently fails
// for array responses).
func listPolls(t *testing.T, ts *testServer, token string) []map[string]interface{} {
	t.Helper()
	status, raw := ts.doRaw(t, "GET", "/api/polls", token)
	if status != http.StatusOK {
		t.Fatalf("list polls: want 200, got %d — %s", status, raw)
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("decode polls list: %v — %s", err, raw)
	}
	return arr
}

// ─── #9 — Question reorder ────────────────────────────────────────────────────

// TestQuestionReorder verifies that PATCH /api/polls/{pollId}/questions/reorder
// persists the new positions and that the subsequent GET reflects them.
func TestQuestionReorder(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "reorder")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "Reorder Poll")
	qPath := "/api/polls/" + pollID + "/questions"

	// Add three open_text questions; positions auto-assigned 1, 2, 3.
	ids := make([]string, 3)
	for i, label := range []string{"first", "second", "third"} {
		_, body := ts.doJSON(t, "POST", qPath, map[string]interface{}{
			"type":               "open_text",
			"text":               label,
			"time_limit_seconds": 30,
			"points":             0,
		}, token)
		ids[i] = getString(body, "id")
		if ids[i] == "" {
			t.Fatalf("create %s: missing id — %v", label, body)
		}
	}

	// New order: third → 1, first → 2, second → 3.
	status, body := ts.doJSON(t, "PATCH", qPath+"/reorder", map[string]interface{}{
		"items": []map[string]interface{}{
			{"id": ids[2], "position": 1},
			{"id": ids[0], "position": 2},
			{"id": ids[1], "position": 3},
		},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("reorder: want 200, got %d — %v", status, body)
	}

	// Refetch and confirm the new sequence.
	listStatus, raw := ts.doRaw(t, "GET", qPath, token)
	if listStatus != http.StatusOK {
		t.Fatalf("list questions: want 200, got %d — %s", listStatus, raw)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode list: %v — %s", err, raw)
	}
	if len(items) != 3 {
		t.Fatalf("list: want 3 questions, got %d", len(items))
	}

	// Build (position, id) pairs sorted by reported position.
	posByID := make(map[string]int, 3)
	for _, it := range items {
		pos := int(it["position"].(float64))
		posByID[getString(it, "id")] = pos
	}
	if posByID[ids[2]] != 1 || posByID[ids[0]] != 2 || posByID[ids[1]] != 3 {
		t.Errorf("unexpected positions after reorder: %v (ids: %v)", posByID, ids)
	}
	t.Logf("✓ reorder persisted: third→1 first→2 second→3")
}

// ─── #22 — Moderation: hide answer + hide brainstorm idea ─────────────────────
//
// Note on what the production frontend actually uses:
//
//   - The HTTP endpoints /api/sessions/{id}/answers/{answerId} and
//     /api/sessions/{id}/ideas/{ideaId} are wired but currently have NO callers
//     in frontend/src — the HostSessionPage moderates ideas via the WebSocket
//     message brainstorm_hide_idea, and word_cloud "hide" is a purely
//     client-side React Set that never reaches the server.
//   - The WS message type "hide_answer" is registered as organizer-only in the
//     dispatcher's auth guard but has no handler in conduct_service.HandleMessage
//     — sending it currently does nothing.
//
// TestModerationHideAnswer covers the HTTP path as a contract regression test
// (the endpoint must keep working if anything starts using it).
// TestBrainstormHideIdeaWS covers the path the frontend actually exercises.
// TestModerationHideIdeaHTTP covers the parallel HTTP idea-hide path.

// TestModerationHideAnswer drives the full HTTP→service→repo path for hiding a
// participant's open_text answer and verifies the broadcast over the live room.
func TestModerationHideAnswer(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "modA")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "Moderation Answer")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "open_text",
			"text":               "What's bothering you?",
			"time_limit_seconds": 60,
			"points":             0,
		}, token)
	questionID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")
	sessionID := getString(body, "session_id")

	part := dialParticipant(t, ts.wsURL(), roomCode, "Sender")
	defer part.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})
	part.waitFor(t, "question_start", 3*time.Second)

	part.send(t, "submit_text", map[string]interface{}{
		"question_id": questionID,
		"text":        "spam content to be hidden",
	})
	part.waitFor(t, "answer_accepted", 3*time.Second)

	// End the question so the room state settles.
	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	part.waitFor(t, "question_end", 3*time.Second)

	// The HTTP API does not expose answer IDs (the export endpoint joins on
	// participant/question and omits the row's UUID, and filters hidden rows).
	// The moderation endpoint, however, requires an answer_id — which the
	// organizer's frontend resolves from internal state. To exercise the
	// HTTP→service→repo path we look up the answer ID directly in the
	// underlying test DB.
	var answerID string
	if err := globalDB.Get(&answerID,
		`SELECT id::text FROM answers WHERE session_id = $1 LIMIT 1`,
		sessionID); err != nil {
		t.Fatalf("locate answer id: %v", err)
	}

	// Hide it via the moderation endpoint.
	hideStatus, _ := ts.doJSON(t, "PATCH",
		fmt.Sprintf("/api/sessions/%s/answers/%s", sessionID, answerID),
		map[string]string{"action": "hide"}, token)
	if hideStatus != http.StatusNoContent {
		t.Fatalf("hide answer: want 204, got %d", hideStatus)
	}

	// All clients in the live room should receive answer_hidden.
	for _, c := range []*wsClient{org, part} {
		msg := c.waitFor(t, "answer_hidden", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if id := getString(data, "answer_id"); id != answerID {
			t.Errorf("answer_hidden id = %q, want %q", id, answerID)
		}
		isHidden, _ := data["is_hidden"].(bool)
		if !isHidden {
			t.Errorf("answer_hidden is_hidden = %v, want true", data["is_hidden"])
		}
	}
	t.Logf("✓ hidden answer broadcast received")

	// Verify the DB column was flipped, and that the export query (which
	// powers CSV / public answers list) drops the hidden row entirely.
	var dbHidden bool
	if err := globalDB.Get(&dbHidden,
		`SELECT is_hidden FROM answers WHERE id = $1`, answerID); err != nil {
		t.Fatalf("read is_hidden: %v", err)
	}
	if !dbHidden {
		t.Errorf("answers.is_hidden = false, want true after hide")
	}

	rawAnswers := getRawAnswers(t, ts, token, sessionID)
	if len(rawAnswers) != 0 {
		t.Errorf("after hide: export answers list should be empty, got %d rows", len(rawAnswers))
	}
}

// TestBrainstormHideIdeaWS exercises the WS path the production HostSessionPage
// actually uses to hide a brainstorm idea. The organizer sends
// brainstorm_hide_idea and every client in the room receives answer_hidden.
//
// This is the *real* user flow for moderation row #22 — so it's worth a focused
// integration test independent of the HTTP-based TestModerationHideIdeaHTTP
// (which covers the same downstream service but a different transport).
func TestBrainstormHideIdeaWS(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "wsmod")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "WS Hide Idea")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "brainstorm",
			"text":               "Ideas",
			"time_limit_seconds": 120,
			"points":             0,
		}, token)
	questionID := getString(body, "id")

	roomCode := createRoom(t, ts, token, pollID)

	part := dialParticipant(t, ts.wsURL(), roomCode, "Author")
	defer part.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})
	part.waitFor(t, "question_start", 3*time.Second)

	part.send(t, "submit_idea", map[string]interface{}{"text": "to be hidden"})
	added := part.waitFor(t, "brainstorm_idea_added", 3*time.Second)
	ideaID := getString(added["data"].(map[string]interface{}), "id")
	org.waitFor(t, "brainstorm_idea_added", 3*time.Second)

	// Organizer hides the idea via WS — same payload the frontend sends.
	org.send(t, "brainstorm_hide_idea", map[string]interface{}{
		"idea_id":   ideaID,
		"is_hidden": true,
	})

	// Both clients receive answer_hidden with idea_id (NOT id — the production
	// frontend currently destructures `id` here, so the broadcast field name
	// is exactly the regression we want to lock down).
	for _, c := range []*wsClient{org, part} {
		msg := c.waitFor(t, "answer_hidden", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if id := getString(data, "idea_id"); id != ideaID {
			t.Errorf("answer_hidden.idea_id = %q, want %q (raw payload: %v)", id, ideaID, data)
		}
		if hidden, _ := data["is_hidden"].(bool); !hidden {
			t.Errorf("answer_hidden.is_hidden = %v, want true", data["is_hidden"])
		}
	}

	// The DB is the source of truth — confirm the row was actually flipped.
	var dbHidden bool
	if err := globalDB.Get(&dbHidden,
		`SELECT is_hidden FROM brainstorm_ideas WHERE id = $1`, ideaID); err != nil {
		t.Fatalf("read idea is_hidden: %v", err)
	}
	if !dbHidden {
		t.Errorf("brainstorm_ideas.is_hidden = false, want true after WS hide")
	}
	t.Logf("✓ WS brainstorm_hide_idea persisted + broadcast")

	// A non-organizer participant must not be able to hide ideas — the WS
	// dispatcher's role gate should reject the message with an error.
	part.send(t, "brainstorm_hide_idea", map[string]interface{}{
		"idea_id":   ideaID,
		"is_hidden": false,
	})
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-part.msgs:
			if msg["type"] == "error" {
				t.Logf("✓ participant brainstorm_hide_idea correctly rejected")
				return
			}
			if msg["type"] == "answer_hidden" {
				if hidden, _ := msg["data"].(map[string]interface{})["is_hidden"].(bool); !hidden {
					t.Fatalf("participant managed to UN-hide idea via WS — auth bypass")
				}
			}
		case <-deadline:
			t.Fatal("expected error response to participant's brainstorm_hide_idea, got none")
		}
	}
}

// TestModerationHideIdeaHTTP exercises the same chain for a brainstorm idea via
// the HTTP path. Currently unused by the frontend (see top-of-section comment)
// but still part of the API surface, so we lock down its contract.
func TestModerationHideIdeaHTTP(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "modI")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "Moderation Idea")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "brainstorm",
			"text":               "Generate ideas",
			"time_limit_seconds": 120,
			"points":             0,
		}, token)
	questionID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")
	sessionID := getString(body, "session_id")

	part := dialParticipant(t, ts.wsURL(), roomCode, "Ideator")
	defer part.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})
	part.waitFor(t, "question_start", 3*time.Second)

	part.send(t, "submit_idea", map[string]interface{}{"text": "spam idea"})
	added := part.waitFor(t, "brainstorm_idea_added", 3*time.Second)
	ideaID := getString(added["data"].(map[string]interface{}), "id")
	// Drain the organizer's mirror copy so the wait below for answer_hidden
	// doesn't accidentally consume an unrelated message.
	org.waitFor(t, "brainstorm_idea_added", 3*time.Second)

	// Hide the idea via the moderation endpoint.
	hideStatus, _ := ts.doJSON(t, "PATCH",
		fmt.Sprintf("/api/sessions/%s/ideas/%s", sessionID, ideaID),
		map[string]string{"action": "hide"}, token)
	if hideStatus != http.StatusNoContent {
		t.Fatalf("hide idea: want 204, got %d", hideStatus)
	}

	// Both organizer and participant should receive answer_hidden referencing
	// the idea (the moderation service reuses the answer_hidden envelope type
	// for both answers and ideas).
	for _, c := range []*wsClient{org, part} {
		msg := c.waitFor(t, "answer_hidden", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if id := getString(data, "idea_id"); id != ideaID {
			t.Errorf("answer_hidden idea_id = %q, want %q", id, ideaID)
		}
		if hidden, _ := data["is_hidden"].(bool); !hidden {
			t.Errorf("answer_hidden is_hidden = %v, want true", data["is_hidden"])
		}
	}
	t.Logf("✓ hidden idea broadcast received")

	// Show again to verify the flag toggles back.
	showStatus, _ := ts.doJSON(t, "PATCH",
		fmt.Sprintf("/api/sessions/%s/ideas/%s", sessionID, ideaID),
		map[string]string{"action": "show"}, token)
	if showStatus != http.StatusNoContent {
		t.Fatalf("show idea: want 204, got %d", showStatus)
	}
	for _, c := range []*wsClient{org, part} {
		msg := c.waitFor(t, "answer_hidden", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if hidden, _ := data["is_hidden"].(bool); hidden {
			t.Errorf("after show: is_hidden = %v, want false", data["is_hidden"])
		}
	}
}

// getRawAnswers fetches /api/sessions/{id}/answers and returns the decoded slice.
func getRawAnswers(t *testing.T, ts *testServer, token, sessionID string) []map[string]interface{} {
	t.Helper()
	status, raw := ts.doRaw(t, "GET", "/api/sessions/"+sessionID+"/answers", token)
	if status != http.StatusOK {
		t.Fatalf("list answers: want 200, got %d — %s", status, raw)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode answers: %v — %s", err, raw)
	}
	return rows
}

// ─── #28 — Participant reconnect with state replay ────────────────────────────

// TestParticipantReconnect verifies that a participant who drops their WS
// connection and reconnects with the saved session_token:
//   - is restored as the same participant (same ID, no duplicate row in DB).
//   - receives a replay of the active question and their own answer_accepted
//     (because they had already answered before disconnecting).
//
// This exercises the ConductService.ReplayStateForClient path through the WS
// onJoin hook, which is otherwise only covered indirectly.
func TestParticipantReconnect(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "reconnect")
	pollID, _ := createPollWithRoom(t, ts, token, "correct_answer", "Reconnect Poll")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "1+1?",
			"time_limit_seconds": 60,
			"points":             100,
			"options": []map[string]interface{}{
				{"text": "1", "is_correct": false},
				{"text": "2", "is_correct": true},
			},
		}, token)
	questionID := getString(body, "id")

	roomCode := createRoom(t, ts, token, pollID)

	// First connection — get the session_token.
	first := dialParticipant(t, ts.wsURL(), roomCode, "Resilient")
	if first.sessionToken == "" {
		t.Fatalf("first connect: missing session_token")
	}
	savedToken := first.sessionToken

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	first.waitFor(t, "question_start", 3*time.Second)
	first.send(t, "submit_answer", map[string]interface{}{
		"question_id": questionID,
		"answer":      1, // correct
	})
	first.waitFor(t, "answer_accepted", 3*time.Second)

	// Drop the first connection.
	first.close()

	// Give the server a moment to clean up the disconnected client without
	// affecting the active question state on the room.
	time.Sleep(150 * time.Millisecond)

	// Reconnect with the saved session_token — same participant must come back.
	second := dialReconnectingParticipant(t, ts.wsURL(), roomCode, savedToken)
	defer second.close()

	if second.sessionToken != savedToken {
		t.Errorf("reconnect: session_token = %q, want %q", second.sessionToken, savedToken)
	}

	// ReplayStateForClient should resend question_start (active question) and
	// answer_accepted (because we already answered). Order is not strictly
	// fixed, so collect both within a window.
	gotQuestion := false
	gotAck := false
	deadline := time.After(3 * time.Second)
	for !(gotQuestion && gotAck) {
		select {
		case msg := <-second.msgs:
			switch msg["type"] {
			case "question_start":
				data, _ := msg["data"].(map[string]interface{})
				if getString(data, "question_id") == questionID {
					gotQuestion = true
				}
			case "answer_accepted":
				data, _ := msg["data"].(map[string]interface{})
				if getString(data, "question_id") == questionID {
					gotAck = true
				}
			}
		case <-deadline:
			t.Fatalf("reconnect replay: gotQuestion=%v gotAck=%v", gotQuestion, gotAck)
		}
	}
	t.Logf("✓ reconnect replayed question_start and answer_accepted")

	// REST endpoint must still show exactly one participant — reconnect must
	// not create a duplicate row.
	status, listBody := ts.doJSON(t, "GET",
		"/api/rooms/"+roomCode+"/participants", nil, token)
	if status != http.StatusOK {
		t.Fatalf("participants list: want 200, got %d", status)
	}
	items, _ := listBody["participants"].([]interface{})
	if len(items) != 1 {
		t.Errorf("after reconnect: want 1 participant row, got %d — %v", len(items), listBody)
	}
}

// dialReconnectingParticipant connects to a room using ?session_token=<uuid>
// (no name parameter) — the server-side handler treats this as a participant
// reconnect when the token matches an existing record in the same session.
func dialReconnectingParticipant(t *testing.T, wsBase, roomCode, sessionToken string) *wsClient {
	t.Helper()
	u := fmt.Sprintf("%s/ws/room/%s?session_token=%s",
		wsBase, roomCode, url.QueryEscape(sessionToken))
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("ws reconnect dial: %v", err)
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

// ─── #32 — PDF export ─────────────────────────────────────────────────────────

// TestSessionPDFExport drives a complete session and asserts that the PDF
// export endpoint returns a valid PDF (correct headers + %PDF magic bytes).
// We don't parse the PDF body — verifying the magic bytes is enough to
// confirm the gofpdf pipeline ran end-to-end.
func TestSessionPDFExport(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "pdf-host")
	pollID, _ := createPollWithRoom(t, ts, token, "correct_answer", "PDF Poll")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "Best framework?",
			"time_limit_seconds": 30,
			"points":             100,
			"options": []map[string]interface{}{
				{"text": "Go", "is_correct": true},
				{"text": "Other", "is_correct": false},
			},
		}, token)
	questionID := getString(body, "id")

	status, body := ts.doJSON(t, "POST", "/api/rooms",
		map[string]interface{}{"poll_id": pollID}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	roomCode := getString(body, "room_code")
	sessionID := getString(body, "session_id")

	part := dialParticipant(t, ts.wsURL(), roomCode, "Reader")
	defer part.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		part.waitFor(t, "question_start", 5*time.Second)
		part.send(t, "submit_answer", map[string]interface{}{
			"question_id": questionID,
			"answer":      0,
		})
		part.waitFor(t, "answer_accepted", 5*time.Second)
	}()
	wg.Wait()

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	part.waitFor(t, "question_end", 5*time.Second)

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end"}, token)
	part.waitFor(t, "session_end", 5*time.Second)

	// — POST the export request with no chart bodies (server fills defaults). —
	reqBody := bytes.NewBufferString(`{"charts": []}`)
	req, err := http.NewRequest("POST",
		ts.baseURL()+"/api/sessions/"+sessionID+"/export/pdf", reqBody)
	if err != nil {
		t.Fatalf("new pdf request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pdf export: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("pdf export: want 200, got %d — %s", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(raw) < 4 || string(raw[:4]) != "%PDF" {
		t.Errorf("response is not a PDF (first 4 bytes: %q)", string(raw[:min(4, len(raw))]))
	}
	t.Logf("✓ PDF export OK (%d bytes)", len(raw))
}

