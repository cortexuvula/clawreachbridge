package proxy

import (
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/canvas"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/media"
)

func TestMediaInspectorAdapterSkipsBinary(t *testing.T) {
	cfg := config.MediaConfig{Enabled: true}
	inj := media.NewInjector(cfg)
	adapter := &mediaInspectorAdapter{injector: inj}

	input := []byte{0x00, 0x01, 0x02}
	result := adapter.InspectMessage(input, websocket.MessageBinary)

	if string(result) != string(input) {
		t.Errorf("binary message should pass through unchanged")
	}
}

func TestMediaInspectorAdapterProcessesText(t *testing.T) {
	cfg := config.MediaConfig{Enabled: true}
	inj := media.NewInjector(cfg)
	adapter := &mediaInspectorAdapter{injector: inj}

	// Non-chat JSON should be returned unchanged by the injector
	input := []byte(`{"type":"ping"}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("non-chat text should pass through unchanged, got %q", result)
	}
}

// noopInspector is a test inspector that records call count.
type noopInspector struct {
	calls int
}

func (n *noopInspector) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	n.calls++
	return payload
}

func TestMessageInspectorInterface(t *testing.T) {
	// Verify noopInspector satisfies MessageInspector
	var _ MessageInspector = &noopInspector{}

	n := &noopInspector{}
	input := []byte(`test`)
	result := n.InspectMessage(input, websocket.MessageText)

	if string(result) != "test" {
		t.Errorf("expected passthrough, got %q", result)
	}
	if n.calls != 1 {
		t.Errorf("expected 1 call, got %d", n.calls)
	}
}

func newTestCanvasTracker() *canvas.CanvasTracker {
	return canvas.NewTracker(config.CanvasConfig{
		StateTracking:   true,
		JSONLBufferSize: 5,
		MaxAge:          5 * time.Minute,
	})
}

func TestCanvasInspectorPassthrough(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{tracker: tr}

	// Canvas message should pass through unchanged
	input := []byte(`{"type":"req","method":"canvas.present","params":{"url":"test"}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("canvas message should pass through unchanged, got %q", result)
	}

	// Verify state was updated
	state := tr.State()
	if !state.Visible {
		t.Error("tracker should be visible after canvas.present")
	}
}

func TestCanvasInspectorIgnoresNonCanvas(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{tracker: tr}

	// Non-canvas message should be ignored
	input := []byte(`{"type":"req","method":"chat.send","params":{"text":"hello"}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("non-canvas message should pass through unchanged")
	}

	state := tr.State()
	if state.Visible {
		t.Error("tracker should not change on non-canvas messages")
	}
}

func TestCanvasInspectorIgnoresBinary(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{tracker: tr}

	input := []byte{0x00, 0x01, 0x02}
	result := adapter.InspectMessage(input, websocket.MessageBinary)

	if string(result) != string(input) {
		t.Errorf("binary message should pass through unchanged")
	}
}

func TestCanvasInspectorIgnoresInvalidJSON(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{tracker: tr}

	input := []byte(`not valid json`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("invalid JSON should pass through unchanged")
	}
}

func TestCanvasInspectorIgnoresNonReqType(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{tracker: tr}

	input := []byte(`{"type":"res","method":"canvas.present"}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("non-req type should pass through unchanged")
	}

	state := tr.State()
	if state.Visible {
		t.Error("tracker should not change on non-req type messages")
	}
}
