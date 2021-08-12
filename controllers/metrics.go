package controllers

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
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(managedFrontendsMetric)
}
