package tracing

import (
	"context"

	"github.com/google/uuid"
)

type (
	tracingIDKey         struct{}
	tracingProcessKey    struct{}
	tracingXRPLTxHashKey struct{}
)

// WithTracingID returns context with set tracing ID.
func WithTracingID(ctx context.Context) context.Context {
	return context.WithValue(ctx, tracingIDKey{}, uuid.New().String())
}

// GetTracingID returns tracing ID from the context.
func GetTracingID(ctx context.Context) string {
	v, ok := ctx.Value(tracingIDKey{}).(string)
	if !ok {
		return ""
	}

	return v
}

// WithTracingProcess returns context with set tracing process.
func WithTracingProcess(ctx context.Context, tracingRoot string) context.Context {
	return context.WithValue(ctx, tracingProcessKey{}, tracingRoot)
}

// GetTracingProcess returns tracing process from the context.
func GetTracingProcess(ctx context.Context) string {
	v, ok := ctx.Value(tracingProcessKey{}).(string)
	if !ok {
		return ""
	}

	return v
}

// WithTracingXRPLTxHash returns context with set XRPL tx hash.
func WithTracingXRPLTxHash(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, tracingXRPLTxHashKey{}, hash)
}

// GetTracingXRPLTxHash returns tracing XRPL tx hash from the context.
func GetTracingXRPLTxHash(ctx context.Context) string {
	v, ok := ctx.Value(tracingXRPLTxHashKey{}).(string)
	if !ok {
		return ""
	}

	return v
}
