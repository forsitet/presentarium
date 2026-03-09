package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/model"
	"presentarium/internal/repository"
	"presentarium/internal/ws"
)

// ParticipantService manages participant lifecycle during live sessions.
type ParticipantService interface {
	// OnJoin is called by the WS handler when a client connects to a room.
	// It creates or restores a participant record, sends the connected message,
	// and notifies the organizer.
	OnJoin(c *ws.Client, room *ws.Room)

	// OnLeave is called by the WS handler when a client disconnects.
	// It updates last_seen and notifies the organizer.
	OnLeave(c *ws.Client, room *ws.Room)

	// ListParticipants returns the participants for the given room (by code).
	ListParticipants(ctx context.Context, code string) ([]*model.Participant, error)
}

// ParticipantInfo is returned by ListParticipants for the REST endpoint.
type ParticipantInfo struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	JoinedAt  time.Time `json:"joined_at"`
	Connected bool      `json:"connected"`
}

type participantService struct {
	participantRepo repository.ParticipantRepository
	sessionRepo     repository.SessionRepository
	hub             *ws.Hub
}

// NewParticipantService creates a new ParticipantService.
func NewParticipantService(
	participantRepo repository.ParticipantRepository,
	sessionRepo repository.SessionRepository,
	hub *ws.Hub,
) ParticipantService {
	return &participantService{
		participantRepo: participantRepo,
		sessionRepo:     sessionRepo,
		hub:             hub,
	}
}

// OnJoin handles participant connection to a room.
// If session_token is already known, the existing participant record is restored.
// Otherwise a new participant is created in the database.
func (s *participantService) OnJoin(c *ws.Client, room *ws.Room) {
	if c.Role() != ws.RoleParticipant {
		// Organizer joined — send a simple connected message.
		sendConnected(c, nil, ws.RoleOrganizer)
		return
	}

	ctx := context.Background()
	sessionID := room.SessionID()

	var participant *model.Participant

	// Attempt reconnect if session_token was provided.
	if token := c.SessionToken(); token != nil {
		existing, err := s.participantRepo.GetBySessionToken(ctx, *token)
		if err == nil && existing.SessionID == sessionID {
			// Restore existing participant.
			participant = existing
			_ = s.participantRepo.UpdateLastSeen(ctx, participant.ID, time.Now().UTC())
		}
	}

	// Create a new participant record if not reconnecting.
	if participant == nil {
		token := c.SessionToken()
		if token == nil {
			t := uuid.New()
			token = &t
		}
		participant = &model.Participant{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Name:         c.Name(),
			SessionToken: *token,
			TotalScore:   0,
			JoinedAt:     time.Now().UTC(),
		}
		if err := s.participantRepo.Create(ctx, participant); err != nil {
			// Log but don't block the connection.
			_ = err
		}
	}

	// Bind participant ID to the WS client for later use.
	c.SetParticipantID(participant.ID)

	// Send connected envelope with session_token.
	sendConnected(c, &participant.SessionToken, ws.RoleParticipant)

	// Notify the organizer that a new participant joined.
	if msg, err := ws.NewEnvelope(ws.MsgTypeParticipantJoined, ws.ParticipantData{
		ID:   participant.ID,
		Name: participant.Name,
	}); err == nil {
		room.SendToOrganizer(msg)
	}
}

// OnLeave handles participant disconnection from a room.
func (s *participantService) OnLeave(c *ws.Client, room *ws.Room) {
	if c.Role() != ws.RoleParticipant {
		return
	}

	ctx := context.Background()

	if pid := c.ParticipantID(); pid != nil {
		_ = s.participantRepo.UpdateLastSeen(ctx, *pid, time.Now().UTC())

		if msg, err := ws.NewEnvelope(ws.MsgTypeParticipantLeft, ws.ParticipantData{
			ID:   *pid,
			Name: c.Name(),
		}); err == nil {
			room.SendToOrganizer(msg)
		}
	}
}

// ListParticipants returns participants for a room identified by its code.
func (s *participantService) ListParticipants(ctx context.Context, code string) ([]*model.Participant, error) {
	session, err := s.sessionRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	// Only the owner's auth is checked at the handler level.
	if session.Status == "finished" {
		// Allow even for finished sessions (historical view).
		_ = session
	}
	return s.participantRepo.ListBySession(ctx, session.ID)
}

// sendConnected sends the {type:"connected"} envelope to the client.
func sendConnected(c *ws.Client, token *uuid.UUID, role ws.ClientRole) {
	data := ws.ConnectedData{Role: string(role)}
	if token != nil {
		data.SessionToken = *token
	}
	if msg, err := ws.NewEnvelope(ws.MsgTypeConnected, data); err == nil {
		c.TrySend(msg)
	}
}
