//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// uploadPresentation uploads a fake .pptx as the authenticated organizer and
// returns its presentation ID once the async conversion worker has marked it
// "ready". The integration suite wires pptxtest.FakeConverter, so conversion
// is essentially instant — we still poll GET /api/presentations/{id} to
// synchronise on the goroutine completion.
func uploadPresentation(t *testing.T, ts *testServer, token, filename, title string) string {
	t.Helper()

	// Build a multipart/form-data body containing a minimally-valid .pptx
	// payload (ZIP magic bytes prefix — enough to pass the handler's
	// magic-byte check; the fake converter does not open it).
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("title", title)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte{0x50, 0x4B, 0x03, 0x04, 'f', 'a', 'k', 'e'}); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = w.Close()

	req, err := http.NewRequest("POST", ts.baseURL()+"/api/presentations", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload presentation: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("upload presentation: want 202, got %d — %s", resp.StatusCode, raw)
	}
	var created map[string]interface{}
	_ = json.Unmarshal(raw, &created)
	id := getString(created, "id")
	if id == "" {
		t.Fatalf("upload response missing id: %s", raw)
	}

	// Poll for status=ready (the fake converter returns 3 slides synchronously
	// inside the worker goroutine).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, body := ts.doJSON(t, "GET", "/api/presentations/"+id, nil, token)
		if status == http.StatusOK && getString(body, "status") == "ready" {
			return id
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("presentation %s did not reach 'ready' in time", id)
	return ""
}

// TestWSPresentation_OpenChangeClose verifies the three presentation WS
// messages: open_presentation, change_slide, and close_presentation.
// It also checks that a participant joining AFTER the presentation was opened
// receives a presentation_opened snapshot so late-joiners restore state.
func TestWSPresentation_OpenChangeClose(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]

	// Register organizer.
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("pres-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Host",
	}, "")
	token := getString(body, "access_token")
	if token == "" {
		t.Fatalf("register: missing access_token — %v", body)
	}

	// Upload a (fake-converted) presentation.
	presID := uploadPresentation(t, ts, token, "deck.pptx", "Demo Deck")

	// Create a poll + room so we have a WS endpoint to connect to.
	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Presentation Flow",
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

	// Dial organizer + first participant.
	org := dialOrganizer(t, ts.wsURL(), roomCode, token)
	defer org.close()
	partA := dialParticipant(t, ts.wsURL(), roomCode, "Alice")
	defer partA.close()

	// Organizer opens the presentation starting at slide 2.
	org.send(t, "open_presentation", map[string]interface{}{
		"presentation_id": presID,
		"slide_position":  2,
	})

	// Both organizer and participant A must see presentation_opened with the
	// full slides snapshot.
	for _, c := range []*wsClient{org, partA} {
		msg := c.waitFor(t, "presentation_opened", 3*time.Second)
		data, ok := msg["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("presentation_opened: missing data — %v", msg)
		}
		if id := getString(data, "presentation_id"); id != presID {
			t.Errorf("presentation_id=%q want %q", id, presID)
		}
		if pos, _ := data["current_slide_position"].(float64); int(pos) != 2 {
			t.Errorf("current_slide_position=%v want 2", data["current_slide_position"])
		}
		slides, _ := data["slides"].([]interface{})
		if len(slides) == 0 {
			t.Errorf("slides list should not be empty — %v", data)
		}
	}

	// Participant B joins AFTER the presentation was opened — must receive a
	// snapshot so they can render the current slide immediately.
	partB := dialParticipant(t, ts.wsURL(), roomCode, "Bob")
	defer partB.close()
	snap := partB.waitFor(t, "presentation_opened", 3*time.Second)
	snapData, _ := snap["data"].(map[string]interface{})
	if pos, _ := snapData["current_slide_position"].(float64); int(pos) != 2 {
		t.Errorf("snapshot current_slide_position=%v want 2", snapData["current_slide_position"])
	}

	// Organizer jumps to slide 3.
	org.send(t, "change_slide", map[string]interface{}{"slide_position": 3})
	for _, c := range []*wsClient{org, partA, partB} {
		msg := c.waitFor(t, "slide_changed", 3*time.Second)
		data, _ := msg["data"].(map[string]interface{})
		if pos, _ := data["slide_position"].(float64); int(pos) != 3 {
			t.Errorf("slide_changed position=%v want 3", data["slide_position"])
		}
	}

	// Out-of-range slide must be rejected with an error back to the organizer
	// only, with no slide_changed broadcast.
	org.send(t, "change_slide", map[string]interface{}{"slide_position": 999})
	org.waitFor(t, "error", 2*time.Second)

	// Organizer closes the presentation.
	org.send(t, "close_presentation", nil)
	for _, c := range []*wsClient{org, partA, partB} {
		c.waitFor(t, "presentation_closed", 3*time.Second)
	}

	// After close, a fresh participant must NOT receive presentation_opened.
	partC := dialParticipant(t, ts.wsURL(), roomCode, "Carol")
	defer partC.close()
	select {
	case msg := <-partC.msgs:
		if msg["type"] == "presentation_opened" {
			t.Errorf("late-joiner received presentation_opened after close: %v", msg)
		}
	case <-time.After(300 * time.Millisecond):
		// OK — no snapshot replayed.
	}
}

// TestWSPresentation_ParticipantCannotOpen verifies the organizer-only guard
// in the WS dispatcher: participants who send open_presentation receive an
// error message, and no presentation_opened is broadcast.
func TestWSPresentation_ParticipantCannotOpen(t *testing.T) {
	truncateAll(t)
	ts := buildTestServer(t)

	suffix := uuid.New().String()[:8]
	_, body := ts.doJSON(t, "POST", "/api/auth/register", map[string]string{
		"email":    fmt.Sprintf("guard-%s@integration.test", suffix),
		"password": "password123",
		"name":     "Host",
	}, "")
	token := getString(body, "access_token")
	presID := uploadPresentation(t, ts, token, "deck.pptx", "Guard Test")

	_, body = ts.doJSON(t, "POST", "/api/polls", map[string]interface{}{
		"title":        "Guard",
		"scoring_rule": "none",
	}, token)
	pollID := getString(body, "id")
	_, body = ts.doJSON(t, "POST", "/api/rooms", map[string]interface{}{
		"poll_id": pollID,
	}, token)
	roomCode := getString(body, "room_code")

	part := dialParticipant(t, ts.wsURL(), roomCode, "Mallory")
	defer part.close()

	part.send(t, "open_presentation", map[string]interface{}{
		"presentation_id": presID,
	})

	// Must receive an error, not presentation_opened.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-part.msgs:
			switch msg["type"] {
			case "error":
				return // expected
			case "presentation_opened":
				t.Fatalf("participant should NOT have opened a presentation — %v", msg)
			}
		case <-deadline:
			t.Fatal("no error received in response to unauthorised open_presentation")
		}
	}
}
