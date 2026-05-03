//go:build integration

package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─── Helpers shared by the additional integration scenarios ────────────────────

// registerOrganizer registers a fresh user with a unique email and returns the
// access token. Used by every scenario below to avoid duplicating boilerplate.
func registerOrganizer(t *testing.T, ts *testServer, namePrefix string) string {
	t.Helper()
	suffix := uuid.New().String()[:8]
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("%s-%s@integration.test", namePrefix, suffix),
		"password": "password123",
		"name":     namePrefix,
	}, "")
	token := getString(body, "access_token")
	if token == "" {
		t.Fatalf("register: missing access_token — %v", body)
	}
	return token
}

// createPollWithRoom creates a poll, returns (pollID, roomCode).
func createPollWithRoom(
	t *testing.T, ts *testServer, token, scoringRule, title string,
) (string, string) {
	t.Helper()
	_, body := ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":          title,
		"scoring_rule":   scoringRule,
		"question_order": "sequential",
	}, token)
	pollID := getString(body, "id")
	if pollID == "" {
		t.Fatalf("create poll: missing id — %v", body)
	}
	return pollID, ""
}

// createRoom creates a fresh room for an existing poll and returns the code.
func createRoom(t *testing.T, ts *testServer, token, pollID string) string {
	t.Helper()
	status, body := ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create room: want 201, got %d — %v", status, body)
	}
	code := getString(body, "room_code")
	if code == "" {
		t.Fatalf("create room: missing room_code — %v", body)
	}
	return code
}

// startSession transitions the room from waiting to active via the HTTP API.
func startSession(t *testing.T, ts *testServer, roomCode, token string) {
	t.Helper()
	status, body := ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "start"}, token)
	if status != http.StatusOK {
		t.Fatalf("start session: want 200, got %d — %v", status, body)
	}
}

// ─── #11 — Timer auto-expiry ──────────────────────────────────────────────────

// TestWSTimerAutoExpiry verifies that the server-side countdown timer fires
// question_end + results + leaderboard automatically when the time limit
// elapses, without any organizer-issued end_question call.
//
// Uses the minimum allowed time_limit_seconds (5) so the test takes ~5 s.
func TestWSTimerAutoExpiry(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "timer-host")
	pollID, _ := createPollWithRoom(t, ts, token, "correct_answer", "Timer Auto-Finish")

	// Question with 5-second limit (the minimum allowed by validation).
	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "Pick A",
			"time_limit_seconds": 5,
			"points":             100,
			"options": []map[string]interface{}{
				{"text": "A", "is_correct": true},
				{"text": "B", "is_correct": false},
			},
		}, token)
	questionID := getString(body, "id")
	if questionID == "" {
		t.Fatalf("create question: missing id — %v", body)
	}

	roomCode := createRoom(t, ts, token, pollID)

	part := dialParticipant(t, ts.wsURL(), roomCode, "Timekeeper")
	defer part.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	// Confirm the question began.
	part.waitFor(t, "question_start", 3*time.Second)

	// At least one timer_tick must arrive before the deadline so we know the
	// server-side countdown is actually running.
	part.waitFor(t, "timer_tick", 3*time.Second)

	// Without sending end_question, wait for the timer to expire on its own.
	// 5 s limit + processing slack = generous 10 s timeout.
	part.waitFor(t, "question_end", 10*time.Second)
	t.Logf("✓ question_end auto-fired after timer expiry")

	// The expired question must reject any further answer submissions: the
	// active question is cleared by finishQuestion, so submit_answer responds
	// with an error rather than answer_accepted.
	part.send(t, "submit_answer", map[string]interface{}{
		"question_id": questionID,
		"answer":      0,
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-part.msgs:
			switch msg["type"] {
			case "answer_accepted":
				t.Fatalf("late answer accepted after timer expired: %v", msg)
			case "error":
				t.Logf("✓ post-expiry submit_answer rejected with error")
				return
			}
		case <-deadline:
			t.Fatal("expected error response to post-expiry submit_answer, got none")
		}
	}
}

// ─── #12 — Answer distribution / results ──────────────────────────────────────

// TestWSAnswerDistribution verifies that finishing a question broadcasts a
// `results` envelope whose answer_counts map matches what participants picked.
// This is the integration check behind the host-side "diagram of answers".
func TestWSAnswerDistribution(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "dist-host")
	pollID, _ := createPollWithRoom(t, ts, token, "correct_answer", "Distribution Poll")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "Pick a colour",
			"time_limit_seconds": 30,
			"points":             50,
			"options": []map[string]interface{}{
				{"text": "Red", "is_correct": true},
				{"text": "Blue", "is_correct": false},
				{"text": "Green", "is_correct": false},
			},
		}, token)
	questionID := getString(body, "id")

	roomCode := createRoom(t, ts, token, pollID)

	// 3 participants pick 0, 0, 1 — distribution should be {"0": 2, "1": 1}.
	picks := []struct {
		name   string
		answer int
	}{
		{"P1", 0},
		{"P2", 0},
		{"P3", 1},
	}
	parts := make([]*wsClient, len(picks))
	for i, p := range picks {
		parts[i] = dialParticipant(t, ts.wsURL(), roomCode, p.name)
		defer parts[i].close()
	}

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	var wg sync.WaitGroup
	for i, p := range picks {
		wg.Add(1)
		go func(c *wsClient, ans int) {
			defer wg.Done()
			c.waitFor(t, "question_start", 5*time.Second)
			c.send(t, "submit_answer", map[string]interface{}{
				"question_id": questionID,
				"answer":      ans,
			})
			c.waitFor(t, "answer_accepted", 5*time.Second)
		}(parts[i], p.answer)
	}
	wg.Wait()

	// End the question early so we don't have to wait for the full timer.
	status, _ := ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	if status != http.StatusOK {
		t.Fatalf("end_question: want 200, got %d", status)
	}

	// The organizer's `results` envelope is what populates the bar chart.
	resMsg := org.waitFor(t, "results", 5*time.Second)
	resData, _ := resMsg["data"].(map[string]interface{})
	if got := getString(resData, "question_id"); got != questionID {
		t.Fatalf("results.question_id = %q, want %q", got, questionID)
	}
	counts, _ := resData["answer_counts"].(map[string]interface{})
	if counts == nil {
		t.Fatalf("results: missing answer_counts — %v", resData)
	}

	// JSON numbers come through as float64.
	gotZero := int(counts["0"].(float64))
	gotOne := int(counts["1"].(float64))
	if gotZero != 2 || gotOne != 1 {
		t.Fatalf("answer_counts: want {0:2, 1:1}, got %v", counts)
	}
	if _, present := counts["2"]; present {
		t.Errorf("answer_counts: unexpected entry for option 2 — %v", counts)
	}
	t.Logf("✓ results distribution: %v", counts)
}

// ─── #14 — Word cloud ─────────────────────────────────────────────────────────

// TestWSWordCloud verifies the multi-submission word_cloud flow:
//   - participant submits two phrases, each producing answer_accepted.
//   - organizer receives wordcloud_update snapshots whose words/counts grow.
//   - case-insensitive aggregation keeps duplicates collapsed under one entry.
func TestWSWordCloud(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "cloud-host")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "Word Cloud Poll")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "word_cloud",
			"text":               "Назовите язык программирования",
			"time_limit_seconds": 60,
			"points":             0,
		}, token)
	questionID := getString(body, "id")

	roomCode := createRoom(t, ts, token, pollID)

	alice := dialParticipant(t, ts.wsURL(), roomCode, "Alice")
	defer alice.close()
	bob := dialParticipant(t, ts.wsURL(), roomCode, "Bob")
	defer bob.close()

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	alice.waitFor(t, "question_start", 3*time.Second)
	bob.waitFor(t, "question_start", 3*time.Second)

	// Alice submits "Go", then "Rust" (two phrases — same row in DB).
	alice.send(t, "submit_text", map[string]interface{}{
		"question_id": questionID, "text": "Go",
	})
	alice.waitFor(t, "answer_accepted", 3*time.Second)
	alice.send(t, "submit_text", map[string]interface{}{
		"question_id": questionID, "text": "Rust",
	})
	alice.waitFor(t, "answer_accepted", 3*time.Second)

	// Bob submits "go" — must aggregate with Alice's "Go" (case-insensitive).
	bob.send(t, "submit_text", map[string]interface{}{
		"question_id": questionID, "text": "go",
	})
	bob.waitFor(t, "answer_accepted", 3*time.Second)

	// Drain wordcloud_update messages on the organizer side and check the
	// final snapshot. There may be several updates — we want the latest.
	var lastWords []map[string]interface{}
	deadline := time.After(3 * time.Second)
collect:
	for {
		select {
		case msg := <-org.msgs:
			if msg["type"] != "wordcloud_update" {
				continue
			}
			data, _ := msg["data"].(map[string]interface{})
			rawWords, _ := data["words"].([]interface{})
			lastWords = nil
			for _, raw := range rawWords {
				if w, ok := raw.(map[string]interface{}); ok {
					lastWords = append(lastWords, w)
				}
			}
		case <-deadline:
			break collect
		}
	}
	if len(lastWords) == 0 {
		t.Fatalf("organizer never received a wordcloud_update with words")
	}

	// Build a map of normalised text → count for assertions.
	got := make(map[string]int, len(lastWords))
	for _, w := range lastWords {
		text := strings.ToLower(strings.TrimSpace(w["text"].(string)))
		got[text] = int(w["count"].(float64))
	}
	if got["go"] != 2 {
		t.Errorf(`expected "go" count = 2 (case-insensitive aggregation), got %d — %v`, got["go"], got)
	}
	if got["rust"] != 1 {
		t.Errorf(`expected "rust" count = 1, got %d — %v`, got["rust"], got)
	}
	t.Logf("✓ wordcloud snapshot: %v", got)
}

// ─── #15 + #16 — Brainstorm flow ──────────────────────────────────────────────

// TestWSBrainstormIdeasAndVotes covers the brainstorm scenario end-to-end:
//   - submit_idea broadcasts brainstorm_idea_added to organizer + submitter.
//   - voting for own idea is rejected with an error.
//   - voting for someone else's idea broadcasts brainstorm_vote_updated.
//   - changing phase to "results" broadcasts brainstorm_phase_changed with a
//     ranked list of ideas.
func TestWSBrainstormIdeasAndVotes(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "bs-host")
	pollID, _ := createPollWithRoom(t, ts, token, "none", "Brainstorm Poll")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "brainstorm",
			"text":               "Идеи для следующего спринта",
			"time_limit_seconds": 120,
			"points":             0,
		}, token)
	questionID := getString(body, "id")

	roomCode := createRoom(t, ts, token, pollID)

	alice := dialParticipant(t, ts.wsURL(), roomCode, "Alice")
	defer alice.close()
	bob := dialParticipant(t, ts.wsURL(), roomCode, "Bob")
	defer bob.close()

	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	alice.waitFor(t, "question_start", 3*time.Second)
	bob.waitFor(t, "question_start", 3*time.Second)

	// Alice and Bob each submit one idea in the collecting phase.
	alice.send(t, "submit_idea", map[string]interface{}{"text": "Refactor auth"})
	aliceIdea := alice.waitFor(t, "brainstorm_idea_added", 3*time.Second)
	aliceIdeaID := getString(aliceIdea["data"].(map[string]interface{}), "id")
	if aliceIdeaID == "" {
		t.Fatalf("alice's brainstorm_idea_added missing id — %v", aliceIdea)
	}
	// Organizer must have seen Alice's idea too.
	gotAliceOnOrg := org.waitFor(t, "brainstorm_idea_added", 3*time.Second)
	if id := getString(gotAliceOnOrg["data"].(map[string]interface{}), "id"); id != aliceIdeaID {
		t.Errorf("organizer received different idea id: got %q want %q", id, aliceIdeaID)
	}

	bob.send(t, "submit_idea", map[string]interface{}{"text": "New onboarding flow"})
	bobIdea := bob.waitFor(t, "brainstorm_idea_added", 3*time.Second)
	bobIdeaID := getString(bobIdea["data"].(map[string]interface{}), "id")
	if bobIdeaID == "" {
		t.Fatalf("bob's brainstorm_idea_added missing id — %v", bobIdea)
	}
	org.waitFor(t, "brainstorm_idea_added", 3*time.Second)

	// Move to voting phase.
	org.send(t, "brainstorm_change_phase", map[string]interface{}{"phase": "voting"})
	for _, c := range []*wsClient{alice, bob} {
		msg := c.waitFor(t, "brainstorm_phase_changed", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if getString(data, "phase") != "voting" {
			t.Errorf("phase_changed: want voting, got %v", data)
		}
	}

	// Alice tries to vote for HER OWN idea — must be rejected.
	alice.send(t, "submit_vote", map[string]interface{}{"idea_id": aliceIdeaID})
	errMsg := alice.waitFor(t, "error", 3*time.Second)
	if data, ok := errMsg["data"].(map[string]interface{}); ok {
		if !strings.Contains(strings.ToLower(getString(data, "message")), "own") {
			t.Errorf("self-vote error message should mention 'own', got %v", data)
		}
	}
	t.Logf("✓ self-vote correctly rejected")

	// Alice votes for Bob's idea — broadcast must reach all clients.
	alice.send(t, "submit_vote", map[string]interface{}{"idea_id": bobIdeaID})
	for _, c := range []*wsClient{alice, bob, org} {
		msg := c.waitFor(t, "brainstorm_vote_updated", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if getString(data, "idea_id") != bobIdeaID {
			t.Errorf("vote_updated.idea_id mismatch: %v", data)
		}
		if int(data["votes_count"].(float64)) != 1 {
			t.Errorf("vote_updated.votes_count: want 1, got %v", data["votes_count"])
		}
	}
	t.Logf("✓ vote broadcast received by everyone")

	// Move to results — broadcast carries a ranked list.
	org.send(t, "brainstorm_change_phase", map[string]interface{}{"phase": "results"})
	resultsMsg := alice.waitFor(t, "brainstorm_phase_changed", 3*time.Second)
	resultsData, _ := resultsMsg["data"].(map[string]interface{})
	if getString(resultsData, "phase") != "results" {
		t.Fatalf("expected phase=results, got %v", resultsData)
	}
	ideas, _ := resultsData["ideas"].([]interface{})
	if len(ideas) != 2 {
		t.Fatalf("results: want 2 ideas, got %d — %v", len(ideas), ideas)
	}
	// First-ranked is Bob (1 vote); second is Alice (0 votes).
	first, _ := ideas[0].(map[string]interface{})
	if getString(first, "id") != bobIdeaID {
		t.Errorf("rank 1: want Bob's idea (1 vote), got %v", first)
	}
	t.Logf("✓ results ranked correctly: %v", ideas)
}

// ─── #17 — Image upload ───────────────────────────────────────────────────────

// TestImageUpload exercises POST /api/upload/image:
//   - a valid PNG (magic bytes) is accepted and a public URL is returned.
//   - a payload that is not a recognised image type is rejected with 415.
func TestImageUpload(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "img-host")

	// Build a 1x1 PNG (just the magic header is enough — the upload handler
	// detects MIME by magic bytes, not by parsing the full image).
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngBytes := append(pngHeader, []byte("rest of payload — not a real PNG body")...)

	// — Valid upload —
	imageURL := postUploadImage(t, ts, token, "tiny.png", pngBytes, http.StatusOK)
	if imageURL == "" {
		t.Fatalf("upload: missing image_url in response")
	}
	if !strings.Contains(imageURL, ".png") {
		t.Errorf("expected returned URL to end in .png, got %q", imageURL)
	}
	t.Logf("✓ valid PNG uploaded → %s", imageURL)

	// — Wrong type (text bytes do not match any allowed magic) —
	postUploadImage(t, ts, token, "evil.png",
		[]byte("not actually an image, just text"),
		http.StatusUnsupportedMediaType)
	t.Logf("✓ non-image payload rejected with 415")

	// — Missing 'image' field on a multipart form —
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("not_image", "x")
	_ = w.Close()
	req, _ := http.NewRequest("POST", ts.baseURL()+"/api/upload/image", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("missing-field upload: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing 'image' field: want 400, got %d", resp.StatusCode)
	}
}

// postUploadImage submits a multipart/form-data POST to /api/upload/image and
// returns the parsed image_url field on success. The wantStatus argument lets
// callers assert both happy- and error-path responses with the same helper.
func postUploadImage(
	t *testing.T, ts *testServer,
	token, filename string, payload []byte, wantStatus int,
) string {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("image", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = w.Close()

	req, err := http.NewRequest("POST", ts.baseURL()+"/api/upload/image", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("upload %s: want %d, got %d — %s", filename, wantStatus, resp.StatusCode, raw)
	}
	if wantStatus != http.StatusOK {
		return ""
	}
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)
	return getString(body, "image_url")
}

// ─── #18 — PPTX upload + processing ───────────────────────────────────────────

// TestPresentationUploadProcessing verifies the HTTP→Service→Storage→DB chain
// for presentation uploads:
//   - POST /api/presentations returns 202 Accepted with status="processing".
//   - the background converter eventually marks the row "ready" with slides.
//   - GET /api/presentations lists the new presentation for the owner.
//   - GET /api/presentations/{id} returns slides with non-empty image URLs.
func TestPresentationUploadProcessing(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "pptx-host")

	// Reuse the multipart helper from presentation_ws_test.go. It already
	// asserts the 202 + polls for status="ready", which is exactly the chain
	// row #18 in the integration table describes.
	presID := uploadPresentation(t, ts, token, "deck.pptx", "Integration Deck")

	// List endpoint must include the new presentation.
	statusList, listBody := ts.doRaw(t, "GET", "/api/presentations", token)
	if statusList != http.StatusOK {
		t.Fatalf("list presentations: want 200, got %d — %s", statusList, listBody)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(listBody, &items); err != nil {
		t.Fatalf("decode list: %v — %s", err, listBody)
	}
	found := false
	for _, it := range items {
		if getString(it, "id") == presID {
			found = true
			if got := getString(it, "status"); got != "ready" {
				t.Errorf("listed presentation status = %q, want ready", got)
			}
		}
	}
	if !found {
		t.Fatalf("uploaded presentation %s not present in /api/presentations", presID)
	}

	// Detail endpoint must expose slides with image URLs.
	statusGet, getBody := ts.doJSON(t, "GET", "/api/presentations/"+presID, nil, token)
	if statusGet != http.StatusOK {
		t.Fatalf("get presentation: want 200, got %d — %v", statusGet, getBody)
	}
	slides, _ := getBody["slides"].([]interface{})
	if len(slides) == 0 {
		t.Fatalf("presentation has no slides — %v", getBody)
	}
	first, _ := slides[0].(map[string]interface{})
	if getString(first, "image_url") == "" {
		t.Errorf("first slide missing image_url — %v", first)
	}
	t.Logf("✓ presentation %s ready with %d slides", presID, len(slides))
}

// ─── #22 — CSV export ─────────────────────────────────────────────────────────

// TestSessionCSVExport drives a minimal session to completion (1 question, 2
// participants, 1 correct + 1 wrong answer) and then asserts the CSV export
// integration: GET /api/sessions/{id}/export/csv returns the right headers,
// the UTF-8 BOM, the column header row, and exactly 2 data rows.
func TestSessionCSVExport(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	token := registerOrganizer(t, ts, "csv-host")
	pollID, _ := createPollWithRoom(t, ts, token, "correct_answer", "CSV Export")

	_, body := ts.doJSON(t, "POST", "/api/polls/"+pollID+"/questions",
		map[string]interface{}{
			"type":               "single_choice",
			"text":               "2 + 2 = ?",
			"time_limit_seconds": 30,
			"points":             100,
			"options": []map[string]interface{}{
				{"text": "3", "is_correct": false},
				{"text": "4", "is_correct": true},
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

	winner := dialParticipant(t, ts.wsURL(), roomCode, "Winner")
	defer winner.close()
	loser := dialParticipant(t, ts.wsURL(), roomCode, "Loser")
	defer loser.close()
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()

	startSession(t, ts, roomCode, token)
	org.send(t, "show_question", map[string]string{"question_id": questionID})

	var wg sync.WaitGroup
	for _, tc := range []struct {
		c   *wsClient
		ans int
	}{
		{winner, 1}, // correct
		{loser, 0},  // wrong
	} {
		wg.Add(1)
		go func(c *wsClient, ans int) {
			defer wg.Done()
			c.waitFor(t, "question_start", 5*time.Second)
			c.send(t, "submit_answer", map[string]interface{}{
				"question_id": questionID,
				"answer":      ans,
			})
			c.waitFor(t, "answer_accepted", 5*time.Second)
		}(tc.c, tc.ans)
	}
	wg.Wait()

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end_question"}, token)
	winner.waitFor(t, "question_end", 5*time.Second)
	loser.waitFor(t, "question_end", 5*time.Second)

	ts.doJSON(t, "PATCH", "/api/rooms/"+roomCode+"/state",
		map[string]string{"action": "end"}, token)
	winner.waitFor(t, "session_end", 5*time.Second)

	// — Export CSV —
	req, _ := http.NewRequest("GET",
		ts.baseURL()+"/api/sessions/"+sessionID+"/export/csv", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("csv export: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("csv export: want 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv*", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}

	raw, _ := io.ReadAll(resp.Body)
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Errorf("CSV missing UTF-8 BOM (first 3 bytes: %x)", raw[:min(3, len(raw))])
	}

	// Strip the BOM, then count non-empty data lines.
	body2 := raw
	if len(body2) >= 3 && body2[0] == 0xEF && body2[1] == 0xBB && body2[2] == 0xBF {
		body2 = body2[3:]
	}
	scanner := bufio.NewScanner(bytes.NewReader(body2))
	var lines []string
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < 1 {
		t.Fatalf("CSV is empty after stripping BOM")
	}
	header := lines[0]
	for _, col := range []string{
		"participant_name", "question_text", "answer",
		"is_correct", "score", "response_time_ms",
	} {
		if !strings.Contains(header, col) {
			t.Errorf("CSV header missing column %q; header: %s", col, header)
		}
	}

	// 2 participants × 1 question = 2 data rows.
	dataRows := len(lines) - 1
	if dataRows != 2 {
		t.Errorf("CSV: want 2 data rows, got %d\n%s", dataRows, body2)
	}

	// Sanity check: the winner row has score=100, the loser row has score=0.
	allRows := strings.Join(lines[1:], "\n")
	if !strings.Contains(allRows, ",100,") {
		t.Errorf("CSV missing score=100 for winner — %s", allRows)
	}
	if !strings.Contains(allRows, ",0,") {
		t.Errorf("CSV missing score=0 for loser — %s", allRows)
	}
	t.Logf("✓ CSV export OK with %d data rows", dataRows)
}

// min keeps a local copy to avoid relying on the builtin `min` (only available
// from Go 1.21+) — the rest of the test suite is conservative about that.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
