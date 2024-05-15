package bridge

import (
	"context"
	"path/filepath"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/crust/build/rust"
)

// BuildSmartContract builds bridge smart contract.
func BuildSmartContract(ctx context.Context, deps build.DepsFunc) error {
	deps(rust.CompileSmartContract(filepath.Join(repoPath, "contract")))
	return nil
}
