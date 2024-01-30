package metrics

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

// New returns new metric set.
func New() *Metrics {
	return &Metrics{
		ErrorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relayer_errors_total",
			Help: "Error counter",
		}),
	}
}

// Metrics contains set of metrics.
type Metrics struct {
	ErrorCounter prometheus.Counter
}

// Register registers all the metrics to prometheus.
func (m *Metrics) Register(registry prometheus.Registerer) error {
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
