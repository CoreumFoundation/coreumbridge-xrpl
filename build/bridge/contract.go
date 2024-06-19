package bridge

import (
	"context"
	"path/filepath"

	"github.com/CoreumFoundation/crust/build/rust"
	"github.com/CoreumFoundation/crust/build/types"
)

// BuildSmartContract builds bridge smart contract.
func BuildSmartContract(ctx context.Context, deps types.DepsFunc) error {
	deps(rust.CompileSmartContract(filepath.Join(repoPath, "contract")))
	return nil
}
