package canvas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/config"
)

func newTestTracker() *CanvasTracker {
	return NewTracker(config.CanvasConfig{
		StateTracking:   true,
		JSONLBufferSize: 3,
		MaxAge:          5 * time.Minute,
	})
}

func TestHandlePresent(t *testing.T) {
	tr := newTestTracker()
	msg := []byte(`{"type":"req","method":"canvas.present","params":{"url":"/__openclaw__/a2ui/?session=xyz"}}`)
	tr.HandleMessage("canvas.present", msg)

	state := tr.State()
	if !state.Visible {
		t.Error("expected visible after canvas.present")
	}
	if state.UpdatedAt.IsZero() {
		t.Error("expected updatedAt to be set")
	}
}

func TestHandleHide(t *testing.T) {
	tr := newTestTracker()
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))
	tr.HandleMessage("canvas.hide", []byte(`{"type":"req","method":"canvas.hide"}`))

	state := tr.State()
	if state.Visible {
		t.Error("expected not visible after canvas.hide")
	}
}

func TestHandlePushJSONL(t *testing.T) {
	tr := newTestTracker()
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))

	tr.HandleMessage("canvas.a2ui.pushJSONL", []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"line1"}}`))
	tr.HandleMessage("canvas.a2ui.pushJSONL", []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"line2"}}`))

	state := tr.State()
	if state.JSONLBuffered != 2 {
		t.Errorf("expected 2 buffered JSONL, got %d", state.JSONLBuffered)
	}
}

func TestJSONLBufferOverflow(t *testing.T) {
	tr := newTestTracker() // buffer size = 3
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))

	for i := 0; i < 5; i++ {
		tr.HandleMessage("canvas.a2ui.pushJSONL", []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"line"}}`))
	}

	state := tr.State()
	if state.JSONLBuffered != 3 {
		t.Errorf("expected buffer capped at 3, got %d", state.JSONLBuffered)
	}

	// Verify oldest entries were dropped (ring buffer behavior)
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if len(tr.jsonlBuffer) != 3 {
		t.Fatalf("internal buffer length = %d, want 3", len(tr.jsonlBuffer))
	}
}

func TestPresentClearsBuffer(t *testing.T) {
	tr := newTestTracker()
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present","params":{"url":"old"}}`))
	tr.HandleMessage("canvas.a2ui.pushJSONL", []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"old-data"}}`))

	if tr.State().JSONLBuffered != 1 {
		t.Fatal("expected 1 buffered before re-present")
	}

	// New present should clear buffer
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present","params":{"url":"new"}}`))

	state := tr.State()
	if state.JSONLBuffered != 0 {
		t.Errorf("expected buffer cleared after re-present, got %d", state.JSONLBuffered)
	}
	if !state.Visible {
		t.Error("expected visible after re-present")
	}
}

// wsEchoServer creates a test WebSocket server that accepts and returns the connection.
func wsEchoServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Logf("accept error: %v", err)
			return
		}
		// Read all messages until closed
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	wsURL := "ws" + server.URL[4:] // http:// -> ws://
	return server, wsURL
}

func TestReplayWhenVisible(t *testing.T) {
	tr := newTestTracker()
	presentMsg := []byte(`{"type":"req","method":"canvas.present","params":{"url":"test"}}`)
	jsonlMsg := []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"line1"}}`)

	tr.HandleMessage("canvas.present", presentMsg)
	tr.HandleMessage("canvas.a2ui.pushJSONL", jsonlMsg)

	// Create a WebSocket server that records received messages
	var received [][]byte
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			mu.Lock()
			received = append(received, data)
			mu.Unlock()
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := tr.ReplayMessages(ctx, conn); err != nil {
		t.Fatalf("ReplayMessages: %v", err)
	}

	// Close and wait for server to process
	conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 replayed messages, got %d", len(received))
	}
	if string(received[0]) != string(presentMsg) {
		t.Errorf("first message = %q, want present msg", received[0])
	}
	if string(received[1]) != string(jsonlMsg) {
		t.Errorf("second message = %q, want jsonl msg", received[1])
	}
}

func TestReplayWhenHidden(t *testing.T) {
	tr := newTestTracker()
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))
	tr.HandleMessage("canvas.hide", []byte(`{"type":"req","method":"canvas.hide"}`))

	server, wsURL := wsEchoServer(t)
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := tr.ReplayMessages(ctx, conn); err != nil {
		t.Fatalf("ReplayMessages: %v", err)
	}
	// No messages should be sent — hidden state
}

func TestReplayWhenStale(t *testing.T) {
	tr := NewTracker(config.CanvasConfig{
		StateTracking:   true,
		JSONLBufferSize: 3,
		MaxAge:          1 * time.Millisecond, // very short for test
	})
	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))

	time.Sleep(5 * time.Millisecond)

	server, wsURL := wsEchoServer(t)
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := tr.ReplayMessages(ctx, conn); err != nil {
		t.Fatalf("ReplayMessages: %v", err)
	}

	state := tr.State()
	if !state.Stale {
		t.Error("expected state to be stale")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tr := newTestTracker()
	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))
			tr.HandleMessage("canvas.a2ui.pushJSONL", []byte(`{"type":"req","method":"canvas.a2ui.pushJSONL","params":{"data":"x"}}`))
			tr.HandleMessage("canvas.hide", []byte(`{"type":"req","method":"canvas.hide"}`))
		}()
	}

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tr.State()
		}()
	}

	wg.Wait()
}

func TestStateStaleFlag(t *testing.T) {
	tr := NewTracker(config.CanvasConfig{
		StateTracking:   true,
		JSONLBufferSize: 3,
		MaxAge:          1 * time.Millisecond,
	})

	// No state yet — not stale (updatedAt is zero)
	state := tr.State()
	if state.Stale {
		t.Error("fresh tracker should not be stale")
	}

	tr.HandleMessage("canvas.present", []byte(`{"type":"req","method":"canvas.present"}`))
	time.Sleep(5 * time.Millisecond)

	state = tr.State()
	if !state.Stale {
		t.Error("expected stale after max_age")
	}
}
