package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNew(t *testing.T) {
	// Reset default registry for test isolation
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg

	m := New()

	if m.ConnectionsTotal == nil {
		t.Error("ConnectionsTotal is nil")
	}
	if m.ActiveConnections == nil {
		t.Error("ActiveConnections is nil")
	}
	if m.MessagesTotal == nil {
		t.Error("MessagesTotal is nil")
	}
	if m.ErrorsTotal == nil {
		t.Error("ErrorsTotal is nil")
	}
	if m.GatewayReachable == nil {
		t.Error("GatewayReachable is nil")
	}

	if m.ReactionsTotal == nil {
		t.Error("ReactionsTotal is nil")
	}
	if m.CanvasEventsTotal == nil {
		t.Error("CanvasEventsTotal is nil")
	}
	if m.CanvasReplaysTotal == nil {
		t.Error("CanvasReplaysTotal is nil")
	}

	// Verify metrics can be used without panic
	m.ConnectionsTotal.Inc()
	m.ActiveConnections.Set(5)
	m.MessagesTotal.WithLabelValues("upstream").Inc()
	m.MessagesTotal.WithLabelValues("downstream").Inc()
	m.ErrorsTotal.WithLabelValues("dial_failure").Inc()
	m.GatewayReachable.Set(1)
	m.ReactionsTotal.WithLabelValues("add").Inc()
	m.ReactionsTotal.WithLabelValues("remove").Inc()
	m.CanvasEventsTotal.WithLabelValues("present").Inc()
	m.CanvasEventsTotal.WithLabelValues("hide").Inc()
	m.CanvasEventsTotal.WithLabelValues("pushJSONL").Inc()
	m.CanvasReplaysTotal.Inc()

	// Verify metrics are gathered
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	expected := []string{
		"clawreachbridge_connections_total",
		"clawreachbridge_active_connections",
		"clawreachbridge_messages_total",
		"clawreachbridge_errors_total",
		"clawreachbridge_gateway_reachable",
		"clawreachbridge_reactions_total",
		"clawreachbridge_canvas_events_total",
		"clawreachbridge_canvas_replays_total",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing metric: %s", name)
		}
	}
}
