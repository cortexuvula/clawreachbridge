package chatsync

import (
	"context"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
)

// ClientEntry represents a connected client on a session.
type ClientEntry struct {
	Conn *websocket.Conn
}

// ClientRegistry tracks WebSocket connections per session for broadcasting.
// Thread-safe via sync.RWMutex.
type ClientRegistry struct {
	mu       sync.RWMutex
	sessions map[string]map[string]*ClientEntry
}

// NewClientRegistry creates an empty registry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		sessions: make(map[string]map[string]*ClientEntry),
	}
}

// Register adds a client to a session.
func (r *ClientRegistry) Register(sessionKey, clientID string, conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sessions[sessionKey] == nil {
		r.sessions[sessionKey] = make(map[string]*ClientEntry)
	}
	r.sessions[sessionKey][clientID] = &ClientEntry{Conn: conn}
	slog.Debug("sync registry: registered", "session", sessionKey, "client", clientID)
}

// Unregister removes a client from a session.
func (r *ClientRegistry) Unregister(sessionKey, clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	clients := r.sessions[sessionKey]
	if clients == nil {
		return
	}
	delete(clients, clientID)
	if len(clients) == 0 {
		delete(r.sessions, sessionKey)
	}
	slog.Debug("sync registry: unregistered", "session", sessionKey, "client", clientID)
}

// Broadcast sends a payload to all clients on a session EXCEPT the sender.
// Takes a snapshot of entries under RLock, then writes without holding the lock.
// coder/websocket Write() serializes internally via mutex, so concurrent
// calls from broadcast + forwarder goroutines are safe.
func (r *ClientRegistry) Broadcast(ctx context.Context, sessionKey, senderID string, payload []byte) {
	r.mu.RLock()
	clients := r.sessions[sessionKey]
	targets := make([]*ClientEntry, 0, len(clients))
	for id, entry := range clients {
		if id != senderID {
			targets = append(targets, entry)
		}
	}
	r.mu.RUnlock()

	for _, entry := range targets {
		if err := entry.Conn.Write(ctx, websocket.MessageText, payload); err != nil {
			slog.Debug("sync broadcast: write failed", "error", err)
		}
	}
}

// ClientCount returns the number of clients on a session.
func (r *ClientRegistry) ClientCount(sessionKey string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions[sessionKey])
}
