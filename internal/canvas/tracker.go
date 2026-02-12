package canvas

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

// TrackerState is a snapshot of the canvas tracker's state for health/debug.
type TrackerState struct {
	Visible       bool      `json:"visible"`
	JSONLBuffered int       `json:"jsonl_buffered"`
	UpdatedAt     time.Time `json:"updated_at"`
	Stale         bool      `json:"stale"`
}

// CanvasTracker shadows canvas state from gateway→client messages
// and replays it to newly connecting clients.
type CanvasTracker struct {
	mu          sync.RWMutex
	visible     bool
	presentMsg  []byte   // full raw bytes of last canvas.present message
	jsonlBuffer [][]byte // ring buffer of full raw canvas.a2ui.pushJSONL messages
	updatedAt   time.Time
	maxAge      time.Duration
	bufferSize  int

	// Optional metrics (nil if metrics disabled)
	eventsTotal  *prometheus.CounterVec
	replaysTotal prometheus.Counter
}

// NewTracker creates a CanvasTracker with the given config.
func NewTracker(cfg config.CanvasConfig) *CanvasTracker {
	return &CanvasTracker{
		bufferSize: cfg.JSONLBufferSize,
		maxAge:     cfg.MaxAge,
	}
}

// SetMetrics attaches Prometheus counters for canvas events and replays.
func (t *CanvasTracker) SetMetrics(events *prometheus.CounterVec, replays prometheus.Counter) {
	t.eventsTotal = events
	t.replaysTotal = replays
}

// HandleMessage updates the canvas state based on the method and raw payload.
// The rawPayload is the full WebSocket message bytes (not parsed/reconstructed).
func (t *CanvasTracker) HandleMessage(method string, rawPayload []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch method {
	case "canvas.present":
		t.presentMsg = append([]byte(nil), rawPayload...)
		t.visible = true
		t.jsonlBuffer = t.jsonlBuffer[:0] // clear buffer on new URL
		t.updatedAt = time.Now()
		slog.Debug("canvas state: present", "payload_size", len(rawPayload))

	case "canvas.hide":
		t.visible = false
		t.updatedAt = time.Now()
		slog.Debug("canvas state: hide")

	case "canvas.a2ui.pushJSONL":
		entry := append([]byte(nil), rawPayload...)
		if len(t.jsonlBuffer) >= t.bufferSize {
			// Ring: drop oldest
			copy(t.jsonlBuffer, t.jsonlBuffer[1:])
			t.jsonlBuffer[len(t.jsonlBuffer)-1] = entry
		} else {
			t.jsonlBuffer = append(t.jsonlBuffer, entry)
		}
		t.updatedAt = time.Now()
		slog.Debug("canvas state: pushJSONL", "buffered", len(t.jsonlBuffer), "payload_size", len(rawPayload))

	default:
		slog.Debug("canvas: untracked method", "method", method)
	}
}

// ReplayMessages writes the shadowed canvas state to a newly connected client.
// Returns nil if there is no state to replay (hidden, stale, or empty).
func (t *CanvasTracker) ReplayMessages(ctx context.Context, conn *websocket.Conn) error {
	t.mu.RLock()
	if !t.visible || t.presentMsg == nil || time.Since(t.updatedAt) > t.maxAge {
		t.mu.RUnlock()
		return nil
	}

	// Copy data under RLock, then release before I/O
	presentCopy := append([]byte(nil), t.presentMsg...)
	jsonlCopies := make([][]byte, len(t.jsonlBuffer))
	for i, buf := range t.jsonlBuffer {
		jsonlCopies[i] = append([]byte(nil), buf...)
	}
	t.mu.RUnlock()

	// Write present message
	if err := conn.Write(ctx, websocket.MessageText, presentCopy); err != nil {
		return err
	}

	// Write buffered JSONL messages
	for _, buf := range jsonlCopies {
		if err := conn.Write(ctx, websocket.MessageText, buf); err != nil {
			return err
		}
	}

	replayCount := 1 + len(jsonlCopies)
	slog.Info("canvas replay injected", "messages", replayCount)

	if t.replaysTotal != nil {
		t.replaysTotal.Inc()
	}

	return nil
}

// State returns a snapshot of the tracker's current state for health/debug endpoints.
func (t *CanvasTracker) State() TrackerState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return TrackerState{
		Visible:       t.visible,
		JSONLBuffered: len(t.jsonlBuffer),
		UpdatedAt:     t.updatedAt,
		Stale:         !t.updatedAt.IsZero() && time.Since(t.updatedAt) > t.maxAge,
	}
}
