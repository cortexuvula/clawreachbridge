package logring

import (
	"context"
	"log/slog"
)

// TeeHandler wraps an inner slog.Handler and also writes log records
// to a RingBuffer for the web UI log viewer.
type TeeHandler struct {
	inner  slog.Handler
	ring   *RingBuffer
	attrs  []slog.Attr
	groups []string
}

// NewTeeHandler creates a handler that forwards to inner and captures to ring.
func NewTeeHandler(inner slog.Handler, ring *RingBuffer) *TeeHandler {
	return &TeeHandler{inner: inner, ring: ring}
}

// Enabled reports whether the handler handles records at the given level.
// Delegates to the inner handler.
func (h *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle processes the record: forwards to inner handler and captures in ring buffer.
func (h *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Capture to ring buffer regardless of inner handler result
	entry := LogEntry{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
	}

	// Collect attributes: pre-set attrs from WithAttrs + record attrs
	attrs := make(map[string]any)

	// Add pre-set attrs (from WithAttrs calls)
	prefix := groupPrefix(h.groups)
	for _, a := range h.attrs {
		attrs[prefix+a.Key] = a.Value.Any()
	}

	// Add record attrs
	r.Attrs(func(a slog.Attr) bool {
		attrs[prefix+a.Key] = a.Value.Any()
		return true
	})

	if len(attrs) > 0 {
		entry.Attrs = attrs
	}

	h.ring.Add(entry)

	// Forward to inner handler
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes pre-set.
func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{
		inner:  h.inner.WithAttrs(attrs),
		ring:   h.ring,
		attrs:  append(cloneAttrs(h.attrs), attrs...),
		groups: h.groups,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *TeeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &TeeHandler{
		inner:  h.inner.WithGroup(name),
		ring:   h.ring,
		attrs:  cloneAttrs(h.attrs),
		groups: append(append([]string{}, h.groups...), name),
	}
}

func cloneAttrs(attrs []slog.Attr) []slog.Attr {
	if attrs == nil {
		return nil
	}
	c := make([]slog.Attr, len(attrs))
	copy(c, attrs)
	return c
}

func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	var p string
	for _, g := range groups {
		p += g + "."
	}
	return p
}
