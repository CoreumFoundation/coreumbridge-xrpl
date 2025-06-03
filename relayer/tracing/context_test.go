package tracing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
)

func TestWithTracingID(t *testing.T) {
	ctx := t.Context()
	ctx = tracing.WithTracingID(ctx)
	tracingID := tracing.GetTracingID(ctx)
	require.NotEmpty(t, tracingID)
}

func TestWithTracingProcess(t *testing.T) {
	ctx := t.Context()
	const process = "pr"
	ctx = tracing.WithTracingProcess(ctx, process)
	gotProcess := tracing.GetTracingProcess(ctx)
	require.Equal(t, process, gotProcess)
}

func TestWithTracingXRPLTxHash(t *testing.T) {
	ctx := t.Context()
	const xrplTxHash = "hash"
	ctx = tracing.WithTracingXRPLTxHash(ctx, xrplTxHash)
	gotXRPLTxHash := tracing.GetTracingXRPLTxHash(ctx)
	require.Equal(t, xrplTxHash, gotXRPLTxHash)
}
