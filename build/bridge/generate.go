package bridge

import (
	"context"

	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

// Generate regenerates everything in bridge.
func Generate(ctx context.Context, deps types.DepsFunc) error {
	deps(tools.EnsureMockgen, golang.Generate)
	return nil
}
