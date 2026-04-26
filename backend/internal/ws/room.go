package ws

import (
	"sync"

	"github.com/google/uuid"
)

// RoomState represents the current state of a live quiz room.
type RoomState string

const (
	StateWaiting         RoomState = "waiting"
	StateActive          RoomState = "active"
	StateShowingQuestion RoomState = "showing_question"
	StateShowingResults  RoomState = "showing_results"
	StateFinished        RoomState = "finished"
)

// ClientRole distinguishes organizer from participant connections.
type ClientRole string

const (
	RoleOrganizer   ClientRole = "organizer"
	RoleParticipant ClientRole = "participant"
)

// ActiveQuestion holds runtime state for the currently displayed question.
type ActiveQuestion struct {
	ID           uuid.UUID
	Type         string // question type: single_choice, multiple_choice, open_text, word_cloud, etc.
	TimeLimitSec int
	StartedAt    int64  // Unix timestamp when the question was shown
	ScoringRule  string // poll-level rule: none | correct_answer | speed_bonus
	Points       int    // question base points
}

// wordCloudEntry is one bucket in the per-question word cloud frequency map.
// Display preserves the original casing/spacing of the first submitter so the
// cloud looks natural ("Go" instead of "go") even though aggregation is
// case-insensitive via the map key.
type wordCloudEntry struct {
	Display string
	Count   int
}

// ActivePresentation holds runtime state for the currently displayed presentation.
// Mirrors the on-disk session row so late-joiners / reconnects can restore the
// current slide without hitting the DB. The Slides slice is a denormalised copy
// of the slide list (including resolved public URLs) so participants who lack
// JWT auth still receive everything they need to render over WS alone.
type ActivePresentation struct {
	ID              uuid.UUID
	Title           string
	SlideCount      int
	CurrentPosition int
	Slides          []SlideInfo
}

// Room represents a live session room managed by the Hub.
type Room struct {
	mu              sync.RWMutex
	code            string
	sessionID       uuid.UUID
	state           RoomState
	clients         map[*Client]bool
	organizer       *Client
	currentQuestion *ActiveQuestion
	// lastQuestion holds the most recently finished question so reconnecting
	// clients can be re-rendered into the showing_results screen even though
	// currentQuestion has been cleared by TryFinishQuestion.
	lastQuestion *ActiveQuestion
	stopTimer    chan struct{}
	answerCount        int                        // number of answers received for the current question
	totalResponseMs    int                        // sum of response times in ms (for average calculation)
	// wordCloudPhrases keys by the normalized phrase ("go", "искусственный
	// интеллект", …) and stores both the original casing/spacing the
	// participant first used (for display) and the running count. Multi-word
	// answers are kept as a single phrase — they are NOT split on spaces.
	wordCloudPhrases map[string]wordCloudEntry

	// questionOrder holds the session-specific order of question IDs.
	// Set once when the session starts (shuffled for "random" polls).
	questionOrder []uuid.UUID

	// Brainstorm in-memory state (reset on each brainstorm question start).
	brainstormPhase      string              // collecting | voting | results
	brainstormIdeaCounts map[uuid.UUID]int   // participantID -> idea count
	brainstormVoteCounts map[uuid.UUID]int   // participantID -> vote count

	// activePresentation holds the currently-opened presentation (nil = none).
	// Protected by the same mutex as the rest of Room state.
	activePresentation *ActivePresentation
}

// newRoom creates a new Room in the waiting state.
func newRoom(code string, sessionID uuid.UUID) *Room {
	return &Room{
		code:      code,
		sessionID: sessionID,
		state:     StateWaiting,
		clients:   make(map[*Client]bool),
	}
}

// AddClient registers a client in the room.
func (r *Room) AddClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[c] = true
	if c.role == RoleOrganizer {
		r.organizer = c
	}
}

// RemoveClient unregisters a client from the room.
func (r *Room) RemoveClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
	if r.organizer == c {
		r.organizer = nil
	}
}

// Broadcast sends a message to every client in the room.
func (r *Room) Broadcast(msg []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		select {
		case c.send <- msg:
		default:
			// Drop message if client buffer is full.
		}
	}
}

// BroadcastToParticipants sends a message to participant clients only.
func (r *Room) BroadcastToParticipants(msg []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		if c.role == RoleParticipant {
			select {
			case c.send <- msg:
			default:
			}
		}
	}
}

// SendToOrganizer sends a message to the organizer client.
func (r *Room) SendToOrganizer(msg []byte) {
	r.mu.RLock()
	o := r.organizer
	r.mu.RUnlock()
	if o != nil {
		select {
		case o.send <- msg:
		default:
		}
	}
}

// SendToClient sends a message to a specific client.
func (r *Room) SendToClient(c *Client, msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// ClientCount returns the total number of connected clients.
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// ParticipantCount returns the number of participant (non-organizer) clients.
func (r *Room) ParticipantCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for c := range r.clients {
		if c.role == RoleParticipant {
			n++
		}
	}
	return n
}

// State returns the current room state.
func (r *Room) State() RoomState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// SetState updates the room state.
func (r *Room) SetState(s RoomState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = s
}

// CurrentQuestion returns the active question info, or nil if none.
func (r *Room) CurrentQuestion() *ActiveQuestion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentQuestion
}

// SetCurrentQuestion sets the active question for this room.
func (r *Room) SetCurrentQuestion(q *ActiveQuestion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentQuestion = q
}

// SetStopTimer stores the channel used to cancel the question timer goroutine.
func (r *Room) SetStopTimer(ch chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopTimer = ch
}

// StopCurrentTimer signals the running timer goroutine to stop.
func (r *Room) StopCurrentTimer() {
	r.mu.Lock()
	ch := r.stopTimer
	r.stopTimer = nil
	r.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SetQuestionOrder stores the ordered list of question IDs for this session.
// Called once when the session starts (after optional shuffle).
func (r *Room) SetQuestionOrder(order []uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]uuid.UUID, len(order))
	copy(cp, order)
	r.questionOrder = cp
}

// GetQuestionOrder returns a copy of the session-specific question order.
func (r *Room) GetQuestionOrder() []uuid.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.questionOrder) == 0 {
		return nil
	}
	cp := make([]uuid.UUID, len(r.questionOrder))
	copy(cp, r.questionOrder)
	return cp
}

// SessionID returns the database session ID for this room.
func (r *Room) SessionID() uuid.UUID {
	return r.sessionID
}

// Code returns the room code.
func (r *Room) Code() string {
	return r.code
}

// Organizer returns the organizer client, or nil.
func (r *Room) Organizer() *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.organizer
}

// TryFinishQuestion atomically checks if the given question is still the active one,
// clears it, and transitions the room to showing_results.
// Returns false if the question was already finished (prevents double-finish).
func (r *Room) TryFinishQuestion(questionID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentQuestion == nil || r.currentQuestion.ID != questionID {
		return false
	}
	r.lastQuestion = r.currentQuestion
	r.currentQuestion = nil
	r.stopTimer = nil
	r.state = StateShowingResults
	return true
}

// LastQuestion returns the most recently finished question, or nil if no
// question has been shown yet. Used to replay state to reconnecting clients
// during the showing_results phase.
func (r *Room) LastQuestion() *ActiveQuestion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastQuestion
}

// ResetAnswerCount resets the in-memory answer counter and response time tracking for a new question.
func (r *Room) ResetAnswerCount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answerCount = 0
	r.totalResponseMs = 0
	r.wordCloudPhrases = nil
}

// InitBrainstorm initialises in-memory state for a new brainstorm question.
func (r *Room) InitBrainstorm() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.brainstormPhase = "collecting"
	r.brainstormIdeaCounts = make(map[uuid.UUID]int)
	r.brainstormVoteCounts = make(map[uuid.UUID]int)
}

// BrainstormPhase returns the current brainstorm phase.
func (r *Room) BrainstormPhase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.brainstormPhase
}

// SetBrainstormPhase updates the brainstorm phase.
func (r *Room) SetBrainstormPhase(phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.brainstormPhase = phase
}

// IncrementBrainstormIdeaCount increments the idea count for a participant and returns the new count.
func (r *Room) IncrementBrainstormIdeaCount(participantID uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.brainstormIdeaCounts == nil {
		r.brainstormIdeaCounts = make(map[uuid.UUID]int)
	}
	r.brainstormIdeaCounts[participantID]++
	return r.brainstormIdeaCounts[participantID]
}

// BrainstormIdeaCount returns the current idea count for a participant.
func (r *Room) BrainstormIdeaCount(participantID uuid.UUID) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.brainstormIdeaCounts[participantID]
}

// IncrementBrainstormVoteCount increments the vote count for a participant and returns the new count.
func (r *Room) IncrementBrainstormVoteCount(participantID uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.brainstormVoteCounts == nil {
		r.brainstormVoteCounts = make(map[uuid.UUID]int)
	}
	r.brainstormVoteCounts[participantID]++
	return r.brainstormVoteCounts[participantID]
}

// BrainstormVoteCount returns the current vote count for a participant.
func (r *Room) BrainstormVoteCount(participantID uuid.UUID) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.brainstormVoteCounts[participantID]
}

// AddWordCloudPhrase records one full phrase submission in the cloud.
//
//	key     — normalized form used for case/whitespace-insensitive aggregation
//	          (e.g. normalize.Text("  ИИ  ") → "ии"). Caller is responsible for
//	          keying so the ws package stays free of normalization deps.
//	display — text shown in the cloud. The first submitter's casing/spacing
//	          is preserved across subsequent submissions of the same phrase.
//
// Returns the new count for this phrase. Empty key is a no-op.
func (r *Room) AddWordCloudPhrase(key, display string) int {
	if key == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wordCloudPhrases == nil {
		r.wordCloudPhrases = make(map[string]wordCloudEntry)
	}
	entry := r.wordCloudPhrases[key]
	if entry.Display == "" {
		entry.Display = display
	}
	entry.Count++
	r.wordCloudPhrases[key] = entry
	return entry.Count
}

// SetWordCloudPhrases replaces the in-memory frequency map. Used when
// rebuilding state from the database after a server restart so reconnecting
// clients see the cloud they had before the restart.
func (r *Room) SetWordCloudPhrases(entries map[string]struct {
	Display string
	Count   int
}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(entries) == 0 {
		r.wordCloudPhrases = nil
		return
	}
	m := make(map[string]wordCloudEntry, len(entries))
	for k, v := range entries {
		m[k] = wordCloudEntry{Display: v.Display, Count: v.Count}
	}
	r.wordCloudPhrases = m
}

// WordCloudTopWords returns the top N phrases by count as (display text, count)
// pairs sorted by count desc then display asc for stability. n <= 0 → all.
func (r *Room) WordCloudTopWords(n int) []WordcloudWord {
	r.mu.RLock()
	src := r.wordCloudPhrases
	r.mu.RUnlock()

	if len(src) == 0 {
		return []WordcloudWord{}
	}

	words := make([]WordcloudWord, 0, len(src))
	for _, entry := range src {
		words = append(words, WordcloudWord{Text: entry.Display, Count: entry.Count})
	}

	// Sort by count descending, then alphabetically for stability.
	sortWordcloudWords(words)

	if n > 0 && n < len(words) {
		words = words[:n]
	}
	return words
}

// sortWordcloudWords sorts words by count descending, then text ascending.
func sortWordcloudWords(words []WordcloudWord) {
	// Simple insertion sort — N is at most 100 for word clouds.
	for i := 1; i < len(words); i++ {
		key := words[i]
		j := i - 1
		for j >= 0 && (words[j].Count < key.Count || (words[j].Count == key.Count && words[j].Text > key.Text)) {
			words[j+1] = words[j]
			j--
		}
		words[j+1] = key
	}
}

// IncrementAnswerCount atomically increments the answer counter and adds the response time.
// Returns the new answer count and the updated average response time in milliseconds.
func (r *Room) IncrementAnswerCount(responseMs int) (count int, avgMs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answerCount++
	r.totalResponseMs += responseMs
	return r.answerCount, r.totalResponseMs / r.answerCount
}

// AnswerCount returns the current answer count for the active question.
func (r *Room) AnswerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.answerCount
}

// ForEachParticipant calls fn for every connected participant client.
// The room mutex is held for reading during iteration, so fn must not
// acquire the same mutex.
func (r *Room) ForEachParticipant(fn func(*Client)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		if c.role == RoleParticipant {
			fn(c)
		}
	}
}

// ActivePresentation returns the currently-open presentation, or nil if none.
// The returned pointer is a defensive copy — mutating it does not affect room
// state (callers should use SetSlidePosition / ClearActivePresentation).
func (r *Room) ActivePresentation() *ActivePresentation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activePresentation == nil {
		return nil
	}
	cp := *r.activePresentation
	// Deep-copy the slides slice so outside callers can't race with writers.
	if len(r.activePresentation.Slides) > 0 {
		cp.Slides = make([]SlideInfo, len(r.activePresentation.Slides))
		copy(cp.Slides, r.activePresentation.Slides)
	}
	return &cp
}

// SetActivePresentation stores the presentation as the currently-open one.
// Replaces any previous presentation state. Passing nil clears the state
// (equivalent to ClearActivePresentation).
func (r *Room) SetActivePresentation(p *ActivePresentation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p == nil {
		r.activePresentation = nil
		return
	}
	cp := *p
	if len(p.Slides) > 0 {
		cp.Slides = make([]SlideInfo, len(p.Slides))
		copy(cp.Slides, p.Slides)
	}
	r.activePresentation = &cp
}

// ClearActivePresentation unsets the currently-open presentation.
func (r *Room) ClearActivePresentation() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activePresentation = nil
}

// SetSlidePosition updates the current slide position on the active
// presentation. Returns false if no presentation is active or if position is
// out of bounds (so callers can send a typed error).
func (r *Room) SetSlidePosition(position int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activePresentation == nil {
		return false
	}
	if position < 1 || position > r.activePresentation.SlideCount {
		return false
	}
	r.activePresentation.CurrentPosition = position
	return true
}
