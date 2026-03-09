// Package ws implements the WebSocket hub for real-time communication.
package ws

import (
	"sync"

	"github.com/google/uuid"
)

// Hub manages all active WebSocket rooms.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room // key: roomCode
}

// NewHub creates and returns a new Hub.
func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]*Room),
	}
}

// CreateRoom creates a new room and registers it in the hub.
// If a room with the same code already exists, it is overwritten.
func (h *Hub) CreateRoom(code string, sessionID uuid.UUID) *Room {
	room := newRoom(code, sessionID)
	h.mu.Lock()
	h.rooms[code] = room
	h.mu.Unlock()
	return room
}

// GetRoom retrieves a room by its code. Returns nil if not found.
func (h *Hub) GetRoom(code string) *Room {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rooms[code]
}

// RemoveRoom deletes a room from the hub.
func (h *Hub) RemoveRoom(code string) {
	h.mu.Lock()
	delete(h.rooms, code)
	h.mu.Unlock()
}

// Register adds a client to the specified room.
// Returns false if the room does not exist.
func (h *Hub) Register(roomCode string, client *Client) bool {
	h.mu.RLock()
	room, ok := h.rooms[roomCode]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	room.AddClient(client)
	return true
}

// Unregister removes a client from its room.
// If the room is finished and empty, it is cleaned up.
func (h *Hub) Unregister(roomCode string, client *Client) {
	h.mu.RLock()
	room, ok := h.rooms[roomCode]
	h.mu.RUnlock()
	if !ok {
		return
	}
	room.RemoveClient(client)
	if room.State() == StateFinished && room.ClientCount() == 0 {
		h.RemoveRoom(roomCode)
	}
}

// Broadcast sends a message to all clients in the specified room.
func (h *Hub) Broadcast(roomCode string, msg []byte) {
	h.mu.RLock()
	room, ok := h.rooms[roomCode]
	h.mu.RUnlock()
	if ok {
		room.Broadcast(msg)
	}
}
