package bridge

import (
	"context"

	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/types"
)

// Generate regenerates everything in bridge.
func Generate(ctx context.Context, deps types.DepsFunc) error {
	return golang.Generate(ctx, deps)
}
