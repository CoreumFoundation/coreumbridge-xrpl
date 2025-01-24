package build

import (
	"context"

	"github.com/CoreumFoundation/coreumbridge-xrpl/build/bridge"
	"github.com/CoreumFoundation/crust/build/crust"
	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

// Commands is a definition of commands available in build system.
var Commands = map[string]types.Command{
	"build/me":   {Fn: crust.BuildBuilder, Description: "Builds the builder"},
	"build/znet": {Fn: crust.BuildZNet, Description: "Builds znet binary"},
	"build": {Fn: func(ctx context.Context, deps types.DepsFunc) error {
		deps(
			bridge.BuildRelayer,
			bridge.BuildSmartContract,
		)
		return nil
	}, Description: "Builds relayer and smart contract"},
	"build/relayer":     {Fn: bridge.BuildRelayer, Description: "Builds relayer"},
	"build/contract":    {Fn: bridge.BuildSmartContract, Description: "Builds smart contract"},
	"fuzz-test":         {Fn: bridge.RunFuzzTests, Description: "Runs fuzz tests"},
	"generate":          {Fn: bridge.Generate, Description: "Generates artifacts"},
	"setup":             {Fn: tools.InstallAll, Description: "Installs all the required tools"},
	"images":            {Fn: bridge.BuildRelayerDockerImage, Description: "Builds relayer docker image"},
	"integration-tests": {Fn: bridge.RunAllIntegrationTests, Description: "Runs integration tests"},
	"integration-tests/xrpl": {Fn: bridge.RunIntegrationTests(bridge.TestXRPL),
		Description: "Runs XRPL integration tests"},
	"integration-tests/processes": {Fn: bridge.RunIntegrationTests(bridge.TestProcesses),
		Description: "Runs processes integration tests"},
	"integration-tests/contract": {Fn: bridge.RunIntegrationTests(bridge.TestContract),
		Description: "Runs smart contract integration tests"},
	"integration-tests/stress": {Fn: bridge.RunIntegrationTests(bridge.TestStress),
		Description: "Runs stress integration tests"},
	"lint":           {Fn: bridge.Lint, Description: "lints code"},
	"release":        {Fn: bridge.ReleaseRelayer, Description: "Releases relayer binary"},
	"release/images": {Fn: bridge.ReleaseRelayerImage, Description: "Releases relayer docker image"},
	"test":           {Fn: golang.Test, Description: "Runs unit tests"},
	"tidy":           {Fn: golang.Tidy, Description: "Runs go mod tidy"},
}
