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
	}
}
