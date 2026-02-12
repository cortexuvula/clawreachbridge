package proxy

import (
	"encoding/json"
	"log/slog"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
)

// ReactionInspector counts reaction messages (chat.react) and records
// Prometheus metrics. It returns the payload unchanged (passthrough mode).
type ReactionInspector struct {
	reactionsTotal *prometheus.CounterVec
}

// NewReactionInspector creates a ReactionInspector that increments the given counter.
func NewReactionInspector(counter *prometheus.CounterVec) *ReactionInspector {
	return &ReactionInspector{reactionsTotal: counter}
}

// reactionEnvelope is the outer JSON structure of a client→gateway request.
type reactionEnvelope struct {
	Type   string          `json:"type"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// reactionParams extracts the action and emoji from chat.react params.
type reactionParams struct {
	Action string `json:"action"`
	Emoji  string `json:"emoji"`
}

// InspectMessage checks if the message is a chat.react request and increments
// the reactions counter. The payload is always returned unchanged.
func (ri *ReactionInspector) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}

	var env reactionEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return payload
	}

	if env.Type != "req" || env.Method != "chat.react" {
		return payload
	}

	action := "unknown"
	var params reactionParams
	if err := json.Unmarshal(env.Params, &params); err == nil && params.Action != "" {
		action = params.Action
	}

	ri.reactionsTotal.WithLabelValues(action).Inc()
	slog.Debug("reaction observed", "action", action, "emoji", params.Emoji)

	return payload
}
