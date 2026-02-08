package logring

import (
	"log/slog"
	"sync"
	"time"
)

// LogEntry represents a single log record stored in the ring buffer.
type LogEntry struct {
	Time    time.Time      `json:"time"`
	Level   slog.Level     `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// RingBuffer is a thread-safe circular buffer for log entries.
type RingBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	head    int  // next write position
	full    bool // whether we've wrapped around
	cap     int
}

// NewRingBuffer creates a new ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		entries: make([]LogEntry, capacity),
		cap:     capacity,
	}
}

// Add appends a log entry to the buffer, overwriting the oldest if full.
func (rb *RingBuffer) Add(entry LogEntry) {
	rb.mu.Lock()
	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.cap
	if rb.head == 0 || (rb.head > 0 && rb.full) {
		rb.full = true
	}
	rb.mu.Unlock()
}

// Entries returns up to limit entries filtered by minimum level and time.
// Results are ordered newest first.
func (rb *RingBuffer) Entries(limit int, minLevel slog.Level, since time.Time) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	n := rb.Len()
	if n == 0 {
		return nil
	}

	// Collect matching entries, walking backwards from newest
	var result []LogEntry
	for i := 0; i < n && (limit <= 0 || len(result) < limit); i++ {
		idx := (rb.head - 1 - i + rb.cap) % rb.cap
		e := rb.entries[idx]
		if e.Level < minLevel {
			continue
		}
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// Len returns the number of entries currently in the buffer.
// Caller must hold at least RLock or call under no contention.
func (rb *RingBuffer) Len() int {
	if rb.full {
		return rb.cap
	}
	return rb.head
}

// Cap returns the buffer capacity.
func (rb *RingBuffer) Cap() int {
	return rb.cap
}
