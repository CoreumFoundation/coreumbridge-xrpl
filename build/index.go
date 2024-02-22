package build

import (
	"context"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/coreumbridge-xrpl/build/bridge"
	"github.com/CoreumFoundation/crust/build/crust"
)

// Commands is a definition of commands available in build system.
var Commands = map[string]build.CommandFunc{
	"build/me": crust.BuildBuilder,
	"build": func(ctx context.Context, deps build.DepsFunc) error {
		deps(
			bridge.BuildRelayer,
			bridge.BuildSmartContract,
		)
		return nil
	},
	"build/relayer":                     bridge.BuildRelayer,
	"build/contract":                    bridge.BuildSmartContract,
	"build/integration-tests":           bridge.BuildAllIntegrationTests,
	"build/integration-tests/xrpl":      bridge.BuildIntegrationTests(bridge.TestXRPL),
	"build/integration-tests/processes": bridge.BuildIntegrationTests(bridge.TestProcesses),
	"build/integration-tests/contract":  bridge.BuildIntegrationTests(bridge.TestContract),
	"generate":                          bridge.Generate,
	"images":                            bridge.BuildRelayerDockerImage,
	"lint":                              bridge.Lint,
	"release":                           bridge.ReleaseRelayer,
	"release/images":                    bridge.ReleaseRelayerImage,
	"test":                              bridge.Test,
	"tidy":                              bridge.Tidy,
}
