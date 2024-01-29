package logger

import (
	"context"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// WithMetrics returns logger reporting metrics.
func WithMetrics(l Logger, registry prometheus.Registerer) (Logger, error) {
	errorCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "relayer_errors_total",
		Help: "Errors counter",
	})
	if err := registry.Register(errorCounter); err != nil {
		return nil, errors.Wrapf(err, "failed to register error сounter")
	}

	return metricLogger{
		parentLogger: l,
		errorCounter: errorCounter,
	}, nil
}

type metricLogger struct {
	parentLogger Logger
	errorCounter prometheus.Counter
}

func (l metricLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	l.parentLogger.Debug(ctx, msg, fields...)
}

func (l metricLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	l.parentLogger.Info(ctx, msg, fields...)
}

func (l metricLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	l.parentLogger.Warn(ctx, msg, fields...)
}

func (l metricLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	l.errorCounter.Inc()
	l.parentLogger.Error(ctx, msg, fields...)
}
