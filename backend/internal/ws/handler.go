package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Origin check is delegated to the CORS middleware / Nginx config.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// MessageHandler is a callback invoked for each valid incoming WS message.
// Implementations live in higher-level services (TASK-017/018).
type MessageHandler func(c *Client, room *Room, env Envelope)

// Handler holds dependencies for WebSocket HTTP endpoints.
type Handler struct {
	hub        *Hub
	jwtSecret  string
	onMessage  MessageHandler // optional; set by conduct service
	onJoin     func(c *Client, room *Room)
	onLeave    func(c *Client, room *Room)
}

// NewHandler creates a WS Handler backed by the given Hub.
func NewHandler(hub *Hub, jwtSecret string) *Handler {
	return &Handler{
		hub:       hub,
		jwtSecret: jwtSecret,
	}
}

// SetMessageHandler registers a callback for incoming messages.
// This allows the conduct service (TASK-017) to hook in without circular deps.
func (h *Handler) SetMessageHandler(fn MessageHandler) {
	h.onMessage = fn
}

// SetJoinLeaveHandlers registers callbacks for participant join/leave events.
func (h *Handler) SetJoinLeaveHandlers(onJoin, onLeave func(c *Client, room *Room)) {
	h.onJoin = onJoin
	h.onLeave = onLeave
}

// HandleRoom upgrades an HTTP request to a WebSocket connection and manages the session.
//
//	GET /ws/room/{code}?name=<name>[&session_token=<uuid>][&token=<jwt>]
//
// Auth:
//   - Organizer:   Authorization: Bearer <jwt>  OR  ?token=<jwt>
//   - Participant: ?session_token=<uuid> (reconnect) or just ?name=<name> (first join)
func (h *Handler) HandleRoom(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		http.Error(w, "missing room code", http.StatusBadRequest)
		return
	}

	room := h.hub.GetRoom(code)
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	// Determine role from JWT.
	role, userID, _ := h.extractJWT(r)

	name := r.URL.Query().Get("name")
	sessionTokenStr := r.URL.Query().Get("session_token")

	if role == RoleParticipant && name == "" && sessionTokenStr == "" {
		http.Error(w, "query param 'name' or 'session_token' required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws upgrade failed", "room", code, "err", err)
		return
	}

	client := NewClient(h.hub, conn, code, role)
	client.name = name

	if role == RoleOrganizer {
		client.userID = userID
	} else {
		if sessionTokenStr != "" {
			if st, err2 := uuid.Parse(sessionTokenStr); err2 == nil {
				client.sessionToken = &st
			}
		}
		// Generate a new session token for first-time participants.
		// The actual DB record is created by the session service in TASK-014.
		if client.sessionToken == nil {
			t := uuid.New()
			client.sessionToken = &t
		}
	}

	if !h.hub.Register(code, client) {
		// Room disappeared between the check and upgrade (race).
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "room closed"))
		conn.Close()
		return
	}

	// Notify the higher-level join handler (set by the session service).
	if h.onJoin != nil {
		h.onJoin(client, room)
	} else {
		// Fallback: send a basic connected message and notify organizer.
		h.defaultJoin(client, room)
	}

	// writePump runs concurrently; readPump blocks the current goroutine.
	go client.writePump()
	client.readPump(h.dispatch)

	// On disconnect: call leave handler.
	if h.onLeave != nil {
		h.onLeave(client, room)
	} else {
		h.defaultLeave(client, room)
	}
}

// dispatch parses an incoming message and calls the registered MessageHandler.
func (h *Handler) dispatch(c *Client, raw []byte) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		slog.Warn("ws invalid json", "room", c.roomCode, "err", err)
		return // Don't close the connection on bad JSON.
	}

	room := h.hub.GetRoom(c.roomCode)
	if room == nil {
		return
	}

	// Basic authorization guard: only organizer may send organizer-only messages.
	switch env.Type {
	case MsgTypeShowQuestion, MsgTypeEndQuestion,
		MsgTypeHideAnswer, MsgTypeBrainstormHideIdea, MsgTypeBrainstormChangePhase:
		if c.role != RoleOrganizer {
			h.sendError(c, "unauthorized: organizer only")
			return
		}
	case MsgTypeSubmitAnswer, MsgTypeSubmitText, MsgTypeSubmitVote, MsgTypeSubmitIdea:
		if c.role != RoleParticipant {
			h.sendError(c, "unauthorized: participant only")
			return
		}
	}

	if h.onMessage != nil {
		h.onMessage(c, room, env)
	}
}

// defaultJoin sends the connected message and notifies the organizer.
// Used when no higher-level join handler has been registered.
func (h *Handler) defaultJoin(c *Client, room *Room) {
	data := ConnectedData{Role: string(c.role)}
	if c.role == RoleParticipant && c.sessionToken != nil {
		data.SessionToken = *c.sessionToken
	}
	if msg, err := NewEnvelope(MsgTypeConnected, data); err == nil {
		select {
		case c.send <- msg:
		default:
		}
	}

	if c.role == RoleParticipant {
		if msg, err := NewEnvelope(MsgTypeParticipantJoined, ParticipantData{Name: c.name}); err == nil {
			room.SendToOrganizer(msg)
		}
	}
}

// defaultLeave notifies the organizer that a participant disconnected.
func (h *Handler) defaultLeave(c *Client, room *Room) {
	if c.role == RoleParticipant {
		if msg, err := NewEnvelope(MsgTypeParticipantLeft, ParticipantData{Name: c.name}); err == nil {
			room.SendToOrganizer(msg)
		}
	}
}

// sendError sends an error envelope to the client.
func (h *Handler) sendError(c *Client, message string) {
	if msg, err := NewEnvelope(MsgTypeError, ErrorData{Message: message}); err == nil {
		select {
		case c.send <- msg:
		default:
		}
	}
}

// extractJWT reads a JWT from the Authorization header or ?token= query param.
// Returns (RoleOrganizer, userID, nil) on success.
func (h *Handler) extractJWT(r *http.Request) (ClientRole, *uuid.UUID, error) {
	tokenStr := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		tokenStr = strings.TrimPrefix(auth, "Bearer ")
	} else if q := r.URL.Query().Get("token"); q != "" {
		tokenStr = q
	}

	if tokenStr == "" {
		return RoleParticipant, nil, jwt.ErrTokenMalformed
	}

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return RoleParticipant, nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return RoleParticipant, nil, jwt.ErrTokenInvalidClaims
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return RoleParticipant, nil, jwt.ErrTokenInvalidClaims
	}

	uid, err := uuid.Parse(sub)
	if err != nil {
		return RoleParticipant, nil, err
	}

	return RoleOrganizer, &uid, nil
}
