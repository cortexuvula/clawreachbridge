package proxy

import (
	"testing"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestReactionInspector(t *testing.T) (*ReactionInspector, *prometheus.CounterVec) {
	t.Helper()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_reactions_total",
		Help: "test",
	}, []string{"action"})
	return NewReactionInspector(counter), counter
}

func TestReactionInspectorCountsAdd(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	msg := []byte(`{"type":"req","method":"chat.react","params":{"action":"add","emoji":"👍"}}`)
	result := ri.InspectMessage(msg, websocket.MessageText)

	if string(result) != string(msg) {
		t.Errorf("payload should pass through unchanged")
	}

	val := testutil.ToFloat64(counter.WithLabelValues("add"))
	if val != 1 {
		t.Errorf("add counter = %v, want 1", val)
	}
}

func TestReactionInspectorCountsRemove(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	msg := []byte(`{"type":"req","method":"chat.react","params":{"action":"remove","emoji":"😂"}}`)
	result := ri.InspectMessage(msg, websocket.MessageText)

	if string(result) != string(msg) {
		t.Errorf("payload should pass through unchanged")
	}

	val := testutil.ToFloat64(counter.WithLabelValues("remove"))
	if val != 1 {
		t.Errorf("remove counter = %v, want 1", val)
	}
}

func TestReactionInspectorIgnoresNonReaction(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	tests := []struct {
		name string
		msg  []byte
	}{
		{"non-req type", []byte(`{"type":"event","event":"chat"}`)},
		{"different method", []byte(`{"type":"req","method":"chat.send"}`)},
		{"invalid JSON", []byte(`not json`)},
		{"empty object", []byte(`{}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ri.InspectMessage(tt.msg, websocket.MessageText)
			if string(result) != string(tt.msg) {
				t.Errorf("non-reaction message should pass through unchanged")
			}
		})
	}

	// None of the above should have incremented any counter
	val := testutil.ToFloat64(counter.WithLabelValues("add"))
	if val != 0 {
		t.Errorf("add counter = %v after non-reaction messages, want 0", val)
	}
}

func TestReactionInspectorSkipsBinary(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	// Even a chat.react payload should be ignored when sent as binary
	msg := []byte(`{"type":"req","method":"chat.react","params":{"action":"add","emoji":"👍"}}`)
	result := ri.InspectMessage(msg, websocket.MessageBinary)

	if string(result) != string(msg) {
		t.Errorf("binary message should pass through unchanged")
	}

	val := testutil.ToFloat64(counter.WithLabelValues("add"))
	if val != 0 {
		t.Errorf("add counter = %v after binary message, want 0", val)
	}
}

func TestReactionInspectorUnknownAction(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	msg := []byte(`{"type":"req","method":"chat.react","params":{"emoji":"👍"}}`)
	result := ri.InspectMessage(msg, websocket.MessageText)

	if string(result) != string(msg) {
		t.Errorf("payload should pass through unchanged")
	}

	val := testutil.ToFloat64(counter.WithLabelValues("unknown"))
	if val != 1 {
		t.Errorf("unknown counter = %v, want 1", val)
	}
}

func TestReactionInspectorMultipleReactions(t *testing.T) {
	ri, counter := newTestReactionInspector(t)

	addMsg := []byte(`{"type":"req","method":"chat.react","params":{"action":"add","emoji":"👍"}}`)
	ri.InspectMessage(addMsg, websocket.MessageText)
	ri.InspectMessage(addMsg, websocket.MessageText)

	removeMsg := []byte(`{"type":"req","method":"chat.react","params":{"action":"remove","emoji":"👍"}}`)
	ri.InspectMessage(removeMsg, websocket.MessageText)

	if v := testutil.ToFloat64(counter.WithLabelValues("add")); v != 2 {
		t.Errorf("add counter = %v, want 2", v)
	}
	if v := testutil.ToFloat64(counter.WithLabelValues("remove")); v != 1 {
		t.Errorf("remove counter = %v, want 1", v)
	}
}

func TestReactionInspectorSatisfiesInterface(t *testing.T) {
	ri, _ := newTestReactionInspector(t)
	var _ MessageInspector = ri
}
