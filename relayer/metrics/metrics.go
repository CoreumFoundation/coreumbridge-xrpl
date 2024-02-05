package metrics

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

// NewRegistry returns new metric registry.
func NewRegistry() *Registry {
	return &Registry{
		ErrorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relayer_errors_total",
			Help: "Error counter",
		}),
	}
}

// Registry contains metrics.
type Registry struct {
	ErrorCounter prometheus.Counter
}

// Register registers all the metrics to prometheus.
func (m *Registry) Register(registry prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.ErrorCounter,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}
