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
// canvas messages. When a2uiURL is set, it rewrites canvas.present params to
// inject the configured URL before passing the payload to the tracker.
type canvasInspectorAdapter struct {
	tracker *canvas.CanvasTracker // nil if state_tracking disabled
	a2uiURL string               // empty = no rewriting
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

	// Rewrite canvas.present to inject A2UI URL
	if env.Method == "canvas.present" && a.a2uiURL != "" {
		if rewritten, err := injectA2UIURL(payload, a.a2uiURL); err == nil {
			payload = rewritten
		} else {
			slog.Warn("canvas inspector: failed to inject a2ui_url", "error", err)
		}
	}

	// Pass (potentially modified) payload to tracker
	if a.tracker != nil {
		a.tracker.HandleMessage(env.Method, payload)
	}
	slog.Debug("canvas inspector: observed", "method", env.Method)

	return payload
}

// injectA2UIURL rewrites a canvas.present JSON message to include
// {"params": {"url": "<url>", ...existing...}}. It preserves all
// top-level fields and any existing params fields.
func injectA2UIURL(payload []byte, url string) ([]byte, error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}

	var params map[string]interface{}
	if raw, ok := msg["params"]; ok {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}
	} else {
		params = make(map[string]interface{})
	}

	params["url"] = url
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	msg["params"] = paramsBytes

	return json.Marshal(msg)
}
