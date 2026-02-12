package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for ClawReach Bridge.
type Metrics struct {
	ConnectionsTotal  prometheus.Counter
	ActiveConnections prometheus.Gauge
	MessagesTotal     *prometheus.CounterVec
	ErrorsTotal       *prometheus.CounterVec
	GatewayReachable  prometheus.Gauge
	ReactionsTotal    *prometheus.CounterVec
	CanvasEventsTotal *prometheus.CounterVec
	CanvasReplaysTotal prometheus.Counter
}

// New creates and registers all Prometheus metrics.
func New() *Metrics {
	return &Metrics{
		ConnectionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "clawreachbridge_connections_total",
			Help: "Total connections handled",
		}),
		ActiveConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "clawreachbridge_active_connections",
			Help: "Current active connections",
		}),
		MessagesTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "clawreachbridge_messages_total",
			Help: "Total messages proxied",
		}, []string{"direction"}),
		ErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "clawreachbridge_errors_total",
			Help: "Total errors",
		}, []string{"type"}),
		GatewayReachable: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "clawreachbridge_gateway_reachable",
			Help: "Gateway reachability (1=up, 0=down)",
		}),
		ReactionsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "clawreachbridge_reactions_total",
			Help: "Total reaction messages observed",
		}, []string{"action"}),
		CanvasEventsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "clawreachbridge_canvas_events_total",
			Help: "Total canvas events observed",
		}, []string{"method"}),
		CanvasReplaysTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "clawreachbridge_canvas_replays_total",
			Help: "Total canvas state replays on reconnect",
		}),
	}
}
