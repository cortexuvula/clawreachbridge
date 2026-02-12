package proxy

import (
	"encoding/json"
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

func TestCanvasInspectorInjectsA2UIURL(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{
		tracker: tr,
		a2uiURL: "http://100.64.0.1:8080/__openclaw__/a2ui/",
	}

	input := []byte(`{"type":"req","method":"canvas.present","params":{}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	var msg map[string]json.RawMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal(msg["params"], &params); err != nil {
		t.Fatalf("params is not valid JSON: %v", err)
	}
	if params["url"] != "http://100.64.0.1:8080/__openclaw__/a2ui/" {
		t.Errorf("params.url = %v, want injected URL", params["url"])
	}

	// Verify tracker also got the modified payload
	state := tr.State()
	if !state.Visible {
		t.Error("tracker should be visible after canvas.present")
	}
}

func TestCanvasInspectorNoInjectionWhenEmpty(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{
		tracker: tr,
		a2uiURL: "", // empty = no rewriting
	}

	input := []byte(`{"type":"req","method":"canvas.present","params":{}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	if string(result) != string(input) {
		t.Errorf("payload should be unchanged when a2uiURL is empty, got %q", result)
	}
}

func TestCanvasInspectorTrackerGetsModifiedPayload(t *testing.T) {
	tr := newTestCanvasTracker()
	adapter := &canvasInspectorAdapter{
		tracker: tr,
		a2uiURL: "http://example.com/a2ui/",
	}

	input := []byte(`{"type":"req","method":"canvas.present","params":{}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	// Verify tracker received the call (state is visible)
	state := tr.State()
	if !state.Visible {
		t.Fatal("tracker should be visible after canvas.present")
	}

	// Verify the returned payload has the injected URL (tracker stores a copy
	// of whatever payload is passed to HandleMessage, so if the returned
	// payload has the URL, the tracker got the modified version too)
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal(msg["params"], &params); err != nil {
		t.Fatalf("params is not valid JSON: %v", err)
	}
	if params["url"] != "http://example.com/a2ui/" {
		t.Errorf("returned payload params.url = %v, want injected URL", params["url"])
	}
}

func TestCanvasInspectorInjectPreservesExistingParams(t *testing.T) {
	adapter := &canvasInspectorAdapter{
		tracker: newTestCanvasTracker(),
		a2uiURL: "http://example.com/a2ui/",
	}

	input := []byte(`{"type":"req","method":"canvas.present","params":{"title":"My Canvas","width":800}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	var msg map[string]json.RawMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal(msg["params"], &params); err != nil {
		t.Fatalf("params is not valid JSON: %v", err)
	}

	if params["url"] != "http://example.com/a2ui/" {
		t.Errorf("params.url = %v, want injected URL", params["url"])
	}
	if params["title"] != "My Canvas" {
		t.Errorf("params.title = %v, want preserved", params["title"])
	}
	// JSON numbers unmarshal as float64
	if params["width"] != float64(800) {
		t.Errorf("params.width = %v, want preserved", params["width"])
	}
}

func TestCanvasInspectorNoTrackerStillRewrites(t *testing.T) {
	adapter := &canvasInspectorAdapter{
		tracker: nil, // no tracker
		a2uiURL: "http://example.com/a2ui/",
	}

	input := []byte(`{"type":"req","method":"canvas.present","params":{}}`)
	result := adapter.InspectMessage(input, websocket.MessageText)

	var msg map[string]json.RawMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal(msg["params"], &params); err != nil {
		t.Fatalf("params is not valid JSON: %v", err)
	}
	if params["url"] != "http://example.com/a2ui/" {
		t.Errorf("params.url = %v, want injected URL", params["url"])
	}
}

func TestInjectA2UIURLFunction(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		url     string
		wantURL string
		wantErr bool
	}{
		{
			name:    "empty params",
			payload: `{"type":"req","method":"canvas.present","params":{}}`,
			url:     "http://example.com/a2ui/",
			wantURL: "http://example.com/a2ui/",
		},
		{
			name:    "existing params preserved",
			payload: `{"type":"req","method":"canvas.present","params":{"title":"test"}}`,
			url:     "http://example.com/a2ui/",
			wantURL: "http://example.com/a2ui/",
		},
		{
			name:    "no params field",
			payload: `{"type":"req","method":"canvas.present"}`,
			url:     "http://example.com/a2ui/",
			wantURL: "http://example.com/a2ui/",
		},
		{
			name:    "malformed JSON",
			payload: `not json`,
			url:     "http://example.com/a2ui/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injectA2UIURL([]byte(tt.payload), tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var msg map[string]json.RawMessage
			if err := json.Unmarshal(result, &msg); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			var params map[string]interface{}
			if err := json.Unmarshal(msg["params"], &params); err != nil {
				t.Fatalf("params is not valid JSON: %v", err)
			}
			if params["url"] != tt.wantURL {
				t.Errorf("params.url = %v, want %q", params["url"], tt.wantURL)
			}
		})
	}
}
