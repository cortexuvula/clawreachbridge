package logring

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer(5)

	if rb.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", rb.Len())
	}
	if rb.Cap() != 5 {
		t.Fatalf("Cap() = %d, want 5", rb.Cap())
	}

	rb.Add(LogEntry{Message: "a", Level: slog.LevelInfo, Time: time.Now()})
	rb.Add(LogEntry{Message: "b", Level: slog.LevelInfo, Time: time.Now()})

	if rb.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", rb.Len())
	}

	entries := rb.Entries(0, slog.LevelDebug, time.Time{})
	if len(entries) != 2 {
		t.Fatalf("Entries() returned %d, want 2", len(entries))
	}
	// Newest first
	if entries[0].Message != "b" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "b")
	}
	if entries[1].Message != "a" {
		t.Errorf("entries[1].Message = %q, want %q", entries[1].Message, "a")
	}
}

func TestRingBufferWrap(t *testing.T) {
	rb := NewRingBuffer(3)

	for i := 0; i < 5; i++ {
		rb.Add(LogEntry{Message: string(rune('a' + i)), Level: slog.LevelInfo, Time: time.Now()})
	}

	if rb.Len() != 3 {
		t.Fatalf("Len() = %d, want 3 (should cap at capacity)", rb.Len())
	}

	entries := rb.Entries(0, slog.LevelDebug, time.Time{})
	if len(entries) != 3 {
		t.Fatalf("Entries() returned %d, want 3", len(entries))
	}
	// Should contain c, d, e (newest first: e, d, c)
	if entries[0].Message != "e" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "e")
	}
	if entries[1].Message != "d" {
		t.Errorf("entries[1].Message = %q, want %q", entries[1].Message, "d")
	}
	if entries[2].Message != "c" {
		t.Errorf("entries[2].Message = %q, want %q", entries[2].Message, "c")
	}
}

func TestRingBufferLevelFilter(t *testing.T) {
	rb := NewRingBuffer(10)

	rb.Add(LogEntry{Message: "debug", Level: slog.LevelDebug, Time: time.Now()})
	rb.Add(LogEntry{Message: "info", Level: slog.LevelInfo, Time: time.Now()})
	rb.Add(LogEntry{Message: "warn", Level: slog.LevelWarn, Time: time.Now()})
	rb.Add(LogEntry{Message: "error", Level: slog.LevelError, Time: time.Now()})

	entries := rb.Entries(0, slog.LevelWarn, time.Time{})
	if len(entries) != 2 {
		t.Fatalf("Entries(minLevel=Warn) returned %d, want 2", len(entries))
	}
	if entries[0].Message != "error" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "error")
	}
	if entries[1].Message != "warn" {
		t.Errorf("entries[1].Message = %q, want %q", entries[1].Message, "warn")
	}
}

func TestRingBufferSinceFilter(t *testing.T) {
	rb := NewRingBuffer(10)

	t0 := time.Now().Add(-10 * time.Second)
	t1 := time.Now().Add(-5 * time.Second)
	t2 := time.Now()

	rb.Add(LogEntry{Message: "old", Level: slog.LevelInfo, Time: t0})
	rb.Add(LogEntry{Message: "mid", Level: slog.LevelInfo, Time: t1})
	rb.Add(LogEntry{Message: "new", Level: slog.LevelInfo, Time: t2})

	since := time.Now().Add(-6 * time.Second)
	entries := rb.Entries(0, slog.LevelDebug, since)
	if len(entries) != 2 {
		t.Fatalf("Entries(since=-6s) returned %d, want 2", len(entries))
	}
	if entries[0].Message != "new" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "new")
	}
}

func TestRingBufferLimit(t *testing.T) {
	rb := NewRingBuffer(10)

	for i := 0; i < 10; i++ {
		rb.Add(LogEntry{Message: string(rune('a' + i)), Level: slog.LevelInfo, Time: time.Now()})
	}

	entries := rb.Entries(3, slog.LevelDebug, time.Time{})
	if len(entries) != 3 {
		t.Fatalf("Entries(limit=3) returned %d, want 3", len(entries))
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := NewRingBuffer(100)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				rb.Add(LogEntry{Message: "msg", Level: slog.LevelInfo, Time: time.Now()})
			}
		}()
	}

	// Concurrent reads
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rb.Entries(10, slog.LevelDebug, time.Time{})
			}
		}()
	}

	wg.Wait()

	// Just verify no panic/race occurred
	if rb.Len() > rb.Cap() {
		t.Errorf("Len() = %d exceeds Cap() = %d", rb.Len(), rb.Cap())
	}
}
