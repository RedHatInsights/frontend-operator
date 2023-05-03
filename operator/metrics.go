package operator

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var managedFrontends = map[string]bool{}

var (
	managedFrontendsMetric = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "frontend_managed_frontends",
			Help: "Frontend Managed Frontends",
		},
	)
	reconciliationRequestMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "frontend_app_reconciliation_requests",
			Help: "Frontend App reconciliation requests",
		},
		[]string{"app"},
	)
	reconciliationTimeMetrics = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "frontend_app_reconciliation_time",
			Help: "Frontend App reconciliation time",
		},
		[]string{"app"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		managedFrontendsMetric,
		reconciliationRequestMetric,
		reconciliationTimeMetrics,
	)
}
