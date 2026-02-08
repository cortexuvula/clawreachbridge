package logring

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestTeeHandlerForwards(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	ring := NewRingBuffer(100)
	handler := NewTeeHandler(inner, ring)

	logger := slog.New(handler)
	logger.Info("hello", "key", "value")

	// Check inner handler received it
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("inner handler did not receive message, got: %s", buf.String())
	}

	// Check ring buffer captured it
	entries := ring.Entries(0, slog.LevelDebug, time.Time{})
	if len(entries) != 1 {
		t.Fatalf("ring has %d entries, want 1", len(entries))
	}
	if entries[0].Message != "hello" {
		t.Errorf("ring entry message = %q, want %q", entries[0].Message, "hello")
	}
	if entries[0].Level != slog.LevelInfo {
		t.Errorf("ring entry level = %v, want %v", entries[0].Level, slog.LevelInfo)
	}
	if v, ok := entries[0].Attrs["key"]; !ok || v != "value" {
		t.Errorf("ring entry attrs[key] = %v, want %q", v, "value")
	}
}

func TestTeeHandlerEnabled(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	ring := NewRingBuffer(100)
	handler := NewTeeHandler(inner, ring)

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("should not be enabled for Debug when inner is Warn")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("should be enabled for Warn")
	}
}

func TestTeeHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	ring := NewRingBuffer(100)
	handler := NewTeeHandler(inner, ring)

	logger := slog.New(handler.WithAttrs([]slog.Attr{slog.String("component", "proxy")}))
	logger.Info("test")

	entries := ring.Entries(0, slog.LevelDebug, time.Time{})
	if len(entries) != 1 {
		t.Fatalf("ring has %d entries, want 1", len(entries))
	}
	if v, ok := entries[0].Attrs["component"]; !ok || v != "proxy" {
		t.Errorf("attrs[component] = %v, want %q", v, "proxy")
	}
}

func TestTeeHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	ring := NewRingBuffer(100)
	handler := NewTeeHandler(inner, ring)

	logger := slog.New(handler.WithGroup("req"))
	logger.Info("test", "method", "GET")

	entries := ring.Entries(0, slog.LevelDebug, time.Time{})
	if len(entries) != 1 {
		t.Fatalf("ring has %d entries, want 1", len(entries))
	}
	if v, ok := entries[0].Attrs["req.method"]; !ok || v != "GET" {
		t.Errorf("attrs[req.method] = %v, want %q", v, "GET")
	}
}
