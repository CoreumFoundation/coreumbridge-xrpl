package logger

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// WithErrorCounterMetric returns logger reporting metrics.
func WithErrorCounterMetric(l Logger, errorCounter prometheus.Counter) (Logger, error) {
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
