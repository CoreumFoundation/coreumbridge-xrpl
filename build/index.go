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
	"build/relayer":     {Fn: bridge.BuildRelayer, Description: "Builds relayer"},
	"build/contract":    {Fn: bridge.BuildSmartContract, Description: "Builds smart contract"},
	"download":          {Fn: bridge.DownloadDependencies, Description: "Downloads go dependencies"},
	"generate":          {Fn: bridge.Generate, Description: "Generates artifacts"},
	"images":            {Fn: bridge.BuildRelayerDockerImage, Description: "Builds relayer docker image"},
	"integration-tests": {Fn: bridge.RunAllIntegrationTests, Description: "Runs integration tests"},
	"integration-tests/xrpl": {Fn: bridge.RunIntegrationTests(bridge.TestXRPL),
		Description: "Runs XRPL integration tests"},
	"integration-tests/processes": {Fn: bridge.RunIntegrationTests(bridge.TestProcesses),
		Description: "Runs processes integration tests"},
	"integration-tests/contract": {Fn: bridge.RunIntegrationTests(bridge.TestContract),
		Description: "Runs smart contract integration tests"},
	"lint":           {Fn: bridge.Lint, Description: "lints code"},
	"release":        {Fn: bridge.ReleaseRelayer, Description: "Releases relayer binary"},
	"release/images": {Fn: bridge.ReleaseRelayerImage, Description: "Releases relayer docker image"},
	"test":           {Fn: bridge.Test, Description: "Runs unit tests"},
	"tidy":           {Fn: bridge.Tidy, Description: "Runs go mod tidy"},
}
