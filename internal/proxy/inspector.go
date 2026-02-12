package proxy

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/canvas"
	"github.com/cortexuvula/clawreachbridge/internal/media"
)

// MessageInspector observes and optionally transforms WebSocket text messages.
// Implementations must be safe for concurrent use.
type MessageInspector interface {
	InspectMessage(payload []byte, msgType websocket.MessageType) []byte
}

// mediaInspectorAdapter wraps media.Injector to satisfy MessageInspector.
type mediaInspectorAdapter struct {
	injector *media.Injector
}

func (a *mediaInspectorAdapter) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}
	return a.injector.ProcessMessage(payload)
}

// canvasInspectorAdapter wraps canvas.CanvasTracker to observe gateway→client
// canvas messages. It never modifies the payload.
type canvasInspectorAdapter struct {
	tracker *canvas.CanvasTracker
}

// canvasEnvelope extracts only the fields needed to identify canvas messages.
type canvasEnvelope struct {
	Type   string `json:"type"`
	Method string `json:"method,omitempty"`
}

func (a *canvasInspectorAdapter) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}

	var env canvasEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return payload
	}

	if env.Type != "req" || !strings.HasPrefix(env.Method, "canvas.") {
		return payload
	}

	a.tracker.HandleMessage(env.Method, payload)
	slog.Debug("canvas inspector: observed", "method", env.Method)

	return payload
}
