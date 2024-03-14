package logger

import (
	"context"

	"go.uber.org/zap"
)

// MetricRegistry is metric registry.
type MetricRegistry interface {
	IncrementRelayerErrorCounter()
}

// WithMetrics returns logger reporting metrics.
func WithMetrics(l Logger, metricRegistry MetricRegistry) (Logger, error) {
	return metricLogger{
		parentLogger:   l,
		metricRegistry: metricRegistry,
	}, nil
}

type metricLogger struct {
	parentLogger   Logger
	metricRegistry MetricRegistry
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
	l.metricRegistry.IncrementRelayerErrorCounter()
	l.parentLogger.Error(ctx, msg, fields...)
}
