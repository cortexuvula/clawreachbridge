package proxy

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/chatsync"
)

// SyncUpstreamInspector intercepts client->gateway messages for cross-device sync.
//   - chat.send: stores user message, broadcasts echo to sibling clients, passes through
//   - sessions.history: responds directly with stored messages, returns nil to suppress forwarding
type SyncUpstreamInspector struct {
	ctx        context.Context
	clientConn *websocket.Conn
	store      *chatsync.MessageStore
	registry   *chatsync.ClientRegistry
	clientID   string

	mu         sync.Mutex
	sessionKey string
}

// NewSyncUpstreamInspector creates an upstream inspector for a single client connection.
func NewSyncUpstreamInspector(
	ctx context.Context,
	clientConn *websocket.Conn,
	store *chatsync.MessageStore,
	registry *chatsync.ClientRegistry,
	clientID string,
) *SyncUpstreamInspector {
	return &SyncUpstreamInspector{
		ctx:        ctx,
		clientConn: clientConn,
		store:      store,
		registry:   registry,
		clientID:   clientID,
	}
}

// SessionKey returns the discovered session key (empty if not yet known).
func (s *SyncUpstreamInspector) SessionKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionKey
}

// Cleanup unregisters this client from the registry.
func (s *SyncUpstreamInspector) Cleanup() {
	s.mu.Lock()
	sk := s.sessionKey
	s.mu.Unlock()
	if sk != "" {
		s.registry.Unregister(sk, s.clientID)
	}
}

func (s *SyncUpstreamInspector) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}

	var env struct {
		Type   string `json:"type"`
		Method string `json:"method,omitempty"`
		ID     string `json:"id,omitempty"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return payload
	}

	if env.Type != "req" {
		return payload
	}

	switch env.Method {
	case "chat.send":
		return s.handleChatSend(payload)
	case "sessions.history":
		return s.handleSessionsHistory(payload, env.ID)
	}

	return payload
}

// chatSendRequest extracts fields from a chat.send request.
type chatSendRequest struct {
	Params struct {
		SessionKey     string `json:"sessionKey"`
		Message        string `json:"message"`
		IdempotencyKey string `json:"idempotencyKey"`
	} `json:"params"`
}

func (s *SyncUpstreamInspector) handleChatSend(payload []byte) []byte {
	var req chatSendRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return payload
	}

	sk := req.Params.SessionKey
	if sk == "" {
		return payload
	}

	s.discoverSession(sk)

	msg := chatsync.StoredMessage{
		ID:        "user-" + req.Params.IdempotencyKey,
		Role:      "user",
		Content:   []chatsync.ContentItem{{Type: "text", Text: req.Params.Message}},
		Timestamp: time.Now().UnixMilli(),
	}
	s.store.Append(sk, msg)

	echo := buildUserEcho(req.Params.IdempotencyKey, req.Params.Message)
	go s.registry.Broadcast(s.ctx, sk, s.clientID, echo)

	slog.Debug("sync: stored + echoed user message", "session", sk, "client", s.clientID)

	return payload
}

// historyRequest extracts fields from a sessions.history request.
type historyRequest struct {
	Params struct {
		SessionKey string `json:"sessionKey"`
		Limit      int    `json:"limit"`
	} `json:"params"`
}

func (s *SyncUpstreamInspector) handleSessionsHistory(payload []byte, requestID string) []byte {
	var req historyRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return payload
	}

	sk := req.Params.SessionKey
	if sk == "" {
		return payload
	}

	s.discoverSession(sk)

	limit := req.Params.Limit
	if limit <= 0 {
		limit = 50
	}

	messages := s.store.GetHistory(sk, limit)
	response := buildHistoryResponse(requestID, messages)

	if err := s.clientConn.Write(s.ctx, websocket.MessageText, response); err != nil {
		slog.Warn("sync: failed to send history response", "error", err)
		return payload // Fall back to forwarding if write fails
	}

	slog.Debug("sync: sent history response", "session", sk, "messages", len(messages))

	return nil // Suppress forwarding to gateway
}

// discoverSession registers the session key on first discovery.
func (s *SyncUpstreamInspector) discoverSession(sk string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionKey == "" {
		s.sessionKey = sk
		s.registry.Register(sk, s.clientID, s.clientConn)
	}
}

// buildUserEcho creates a synthetic chat event echoing a user message to siblings.
func buildUserEcho(idempotencyKey, text string) []byte {
	echo := map[string]interface{}{
		"type":  "event",
		"event": "chat",
		"payload": map[string]interface{}{
			"state": "final",
			"runId": "user-" + idempotencyKey,
			"message": map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": text},
				},
				"attachments": []interface{}{},
			},
		},
	}
	data, _ := json.Marshal(echo)
	return data
}

// buildHistoryResponse creates a sessions.history response from stored messages.
func buildHistoryResponse(requestID string, messages []chatsync.StoredMessage) []byte {
	msgList := make([]map[string]interface{}, len(messages))
	for i, m := range messages {
		msgList[i] = map[string]interface{}{
			"id":        m.ID,
			"role":      m.Role,
			"content":   m.Content,
			"timestamp": m.Timestamp,
		}
	}

	resp := map[string]interface{}{
		"type": "res",
		"id":   requestID,
		"payload": map[string]interface{}{
			"messages": msgList,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

// SyncDownstreamInspector observes gateway->client messages and stores
// completed assistant responses for history retrieval.
type SyncDownstreamInspector struct {
	store      *chatsync.MessageStore
	sessionKey func() string // lazy: session key discovered by upstream inspector
}

// NewSyncDownstreamInspector creates a downstream inspector that stores assistant messages.
func NewSyncDownstreamInspector(store *chatsync.MessageStore, sessionKeyFn func() string) *SyncDownstreamInspector {
	return &SyncDownstreamInspector{
		store:      store,
		sessionKey: sessionKeyFn,
	}
}

func (d *SyncDownstreamInspector) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}

	sk := d.sessionKey()
	if sk == "" {
		return payload
	}

	var env struct {
		Type    string          `json:"type"`
		Event   string          `json:"event,omitempty"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return payload
	}

	if env.Type != "event" || env.Event != "chat" {
		return payload
	}

	var chatPayload struct {
		State   string `json:"state"`
		RunID   string `json:"runId"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(env.Payload, &chatPayload); err != nil {
		return payload
	}

	if chatPayload.State != "final" || chatPayload.Message.Role != "assistant" {
		return payload
	}

	var contentItems []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(chatPayload.Message.Content, &contentItems); err != nil {
		return payload
	}

	var textItems []chatsync.ContentItem
	for _, c := range contentItems {
		if c.Type == "text" {
			textItems = append(textItems, chatsync.ContentItem{Type: "text", Text: c.Text})
		}
	}

	if len(textItems) == 0 {
		return payload
	}

	msg := chatsync.StoredMessage{
		ID:        chatPayload.RunID,
		Role:      "assistant",
		Content:   textItems,
		Timestamp: time.Now().UnixMilli(),
	}
	d.store.Append(sk, msg)

	slog.Debug("sync: stored assistant message", "session", sk, "runId", chatPayload.RunID)

	return payload
}
