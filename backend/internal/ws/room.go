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
	TimeLimitSec int
	StartedAt    int64 // Unix timestamp when the question was shown
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
	stopTimer       chan struct{}
	answerCount     int // number of answers received for the current question
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
	r.currentQuestion = nil
	r.stopTimer = nil
	r.state = StateShowingResults
	return true
}

// ResetAnswerCount resets the in-memory answer counter for a new question.
func (r *Room) ResetAnswerCount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answerCount = 0
}

// IncrementAnswerCount atomically increments and returns the new answer count.
func (r *Room) IncrementAnswerCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answerCount++
	return r.answerCount
}

// AnswerCount returns the current answer count for the active question.
func (r *Room) AnswerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.answerCount
}
