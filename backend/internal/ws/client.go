package ws

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// writeWait is the maximum time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the maximum time to wait for a pong reply after sending a ping.
	pongWait = 60 * time.Second

	// pingPeriod is how often pings are sent. Must be less than pongWait.
	pingPeriod = 30 * time.Second

	// maxMessageSize is the maximum allowed incoming message size (4 KB).
	maxMessageSize = 4 * 1024

	// sendBufSize is the capacity of the client's outgoing message buffer.
	sendBufSize = 256

	// rateLimitMessages is the maximum number of messages allowed per window.
	rateLimitMessages = 10

	// rateLimitWindow is the sliding rate-limit window duration.
	rateLimitWindow = time.Second
)

// Client represents a single WebSocket connection (organizer or participant).
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	roomCode string
	role     ClientRole

	// Set for organizer connections.
	userID *uuid.UUID

	// Set for participant connections.
	participantID *uuid.UUID
	sessionToken  *uuid.UUID
	name          string

	// Rate limiting state.
	rateMu      sync.Mutex
	msgCount    int
	windowStart time.Time
}

// NewClient allocates a Client. Call readPump and writePump in goroutines after creation.
func NewClient(hub *Hub, conn *websocket.Conn, roomCode string, role ClientRole) *Client {
	return &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, sendBufSize),
		roomCode:    roomCode,
		role:        role,
		windowStart: time.Now(),
	}
}

// checkRateLimit returns true if the client is within the allowed rate.
func (c *Client) checkRateLimit() bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	now := time.Now()
	if now.Sub(c.windowStart) >= rateLimitWindow {
		c.msgCount = 0
		c.windowStart = now
	}
	c.msgCount++
	return c.msgCount <= rateLimitMessages
}

// readPump reads messages from the WebSocket and dispatches them via msgHandler.
// It runs until the connection is closed or an error occurs.
// On return it unregisters the client from the hub.
func (c *Client) readPump(msgHandler func(c *Client, msg []byte)) {
	defer func() {
		c.hub.Unregister(c.roomCode, c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseMessageTooBig,
			) {
				slog.Warn("ws read error", "room", c.roomCode, "role", string(c.role), "err", err)
			}
			break
		}

		if !c.checkRateLimit() {
			if errMsg, err2 := NewEnvelope(MsgTypeError, ErrorData{Message: "rate limit exceeded"}); err2 == nil {
				select {
				case c.send <- errMsg:
				default:
				}
			}
			continue
		}

		if msgHandler != nil {
			msgHandler(c, msg)
		}
	}
}

// writePump writes messages from the send channel to the WebSocket.
// It also sends periodic pings to keep the connection alive.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close closes the client's send channel, causing writePump to exit.
func (c *Client) Close() {
	close(c.send)
}

// Role returns the client's role.
func (c *Client) Role() ClientRole {
	return c.role
}

// Name returns the participant's display name.
func (c *Client) Name() string {
	return c.name
}

// SessionToken returns the participant's session token, or nil for organizers.
func (c *Client) SessionToken() *uuid.UUID {
	return c.sessionToken
}

// ParticipantID returns the participant's DB ID, or nil if not yet assigned.
func (c *Client) ParticipantID() *uuid.UUID {
	return c.participantID
}

// SetParticipantID assigns the participant's DB ID (set after DB record creation).
func (c *Client) SetParticipantID(id uuid.UUID) {
	c.participantID = &id
}

// UserID returns the organizer's DB user ID, or nil for participants.
func (c *Client) UserID() *uuid.UUID {
	return c.userID
}

// TrySend attempts a non-blocking send of msg to the client's outgoing buffer.
// Returns false if the buffer is full.
func (c *Client) TrySend(msg []byte) bool {
	select {
	case c.send <- msg:
		return true
	default:
		return false
	}
}
