package logger

import (
	"context"

	"go.uber.org/zap"
)

//go:generate ../../bin/mockgen -destination=mock.go -package=logger . Logger

// Logger is a logger interface.
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...zap.Field)
	Info(ctx context.Context, msg string, fields ...zap.Field)
	Warn(ctx context.Context, msg string, fields ...zap.Field)
	Error(ctx context.Context, msg string, fields ...zap.Field)
}
