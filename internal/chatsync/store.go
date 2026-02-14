package chatsync

import (
	"sync"
)

// ContentItem represents a single content element in a stored message.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// StoredMessage is a chat message retained for cross-device sync.
type StoredMessage struct {
	ID        string        `json:"id"`
	Role      string        `json:"role"`
	Content   []ContentItem `json:"content"`
	Timestamp int64         `json:"timestamp"`
}

// MessageStore is a per-session in-memory ring buffer for chat messages.
// Thread-safe via sync.RWMutex.
type MessageStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionStore
	maxSize  int
}

type sessionStore struct {
	messages []StoredMessage
}

// NewMessageStore creates a store that retains up to maxSize messages per session.
func NewMessageStore(maxSize int) *MessageStore {
	return &MessageStore{
		sessions: make(map[string]*sessionStore),
		maxSize:  maxSize,
	}
}

// Append adds a message to the session's ring buffer.
// When the buffer exceeds maxSize, the oldest message is dropped.
func (s *MessageStore) Append(sessionKey string, msg StoredMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.sessions[sessionKey]
	if !ok {
		ss = &sessionStore{}
		s.sessions[sessionKey] = ss
	}

	ss.messages = append(ss.messages, msg)
	if len(ss.messages) > s.maxSize {
		// Drop oldest to stay within maxSize
		excess := len(ss.messages) - s.maxSize
		ss.messages = ss.messages[excess:]
	}
}

// GetHistory returns up to limit messages for a session in chronological order.
// Returns nil if the session has no stored messages.
func (s *MessageStore) GetHistory(sessionKey string, limit int) []StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ss, ok := s.sessions[sessionKey]
	if !ok {
		return nil
	}

	msgs := ss.messages
	if limit > 0 && limit < len(msgs) {
		msgs = msgs[len(msgs)-limit:]
	}

	result := make([]StoredMessage, len(msgs))
	copy(result, msgs)
	return result
}

// Count returns the number of stored messages for a session.
func (s *MessageStore) Count(sessionKey string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ss, ok := s.sessions[sessionKey]
	if !ok {
		return 0
	}
	return len(ss.messages)
}
