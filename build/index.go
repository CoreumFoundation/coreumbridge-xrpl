package build

import (
	"context"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/coreumbridge-xrpl/build/bridge"
	"github.com/CoreumFoundation/crust/build/crust"
)

// Commands is a definition of commands available in build system.
var Commands = map[string]build.Command{
	"build/me": {Fn: crust.BuildBuilder, Description: "Builds the builder"},
	"build": {Fn: func(ctx context.Context, deps build.DepsFunc) error {
		deps(
			bridge.BuildRelayer,
			bridge.BuildSmartContract,
		)
		return nil
	}, Description: "Builds relayer and smart contract"},
	"build/relayer":           {Fn: bridge.BuildRelayer, Description: "Builds relayer"},
	"build/contract":          {Fn: bridge.BuildSmartContract, Description: "Builds smart contract"},
	"build/integration-tests": {Fn: bridge.BuildAllIntegrationTests, Description: "Builds integration tests"},
	"build/integration-tests/xrpl": {Fn: bridge.BuildIntegrationTests(bridge.TestXRPL),
		Description: "Builds XRPL integration tests"},
	"build/integration-tests/processes": {Fn: bridge.BuildIntegrationTests(bridge.TestProcesses),
		Description: "Builds processes integration tests"},
	"build/integration-tests/contract": {Fn: bridge.BuildIntegrationTests(bridge.TestContract),
		Description: "Builds smart contract integration tests"},
	"download":       {Fn: bridge.DownloadDependencies, Description: "Downloads go dependencies"},
	"generate":       {Fn: bridge.Generate, Description: "Generates artifacts"},
	"images":         {Fn: bridge.BuildRelayerDockerImage, Description: "Builds relayer docker image"},
	"lint":           {Fn: bridge.Lint, Description: "lints code"},
	"release":        {Fn: bridge.ReleaseRelayer, Description: "Releases relayer binary"},
	"release/images": {Fn: bridge.ReleaseRelayerImage, Description: "Releases relayer docker image"},
	"test":           {Fn: bridge.Test, Description: "Runs unit tests"},
	"tidy":           {Fn: bridge.Tidy, Description: "Runs go mod tidy"},
}
