package bridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/CoreumFoundation/coreum/build/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/build/tools"
	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/types"
	"github.com/CoreumFoundation/crust/znet/infra"
	"github.com/CoreumFoundation/crust/znet/infra/apps"
	"github.com/CoreumFoundation/crust/znet/pkg/znet"
)

// Test names.
const (
	TestContract  = "contract"
	TestProcesses = "processes"
	TestXRPL      = "xrpl"
	TestStress    = "stress"
)

// RunAllIntegrationTests runs all the bridge integration tests.
func RunAllIntegrationTests(ctx context.Context, deps types.DepsFunc) error {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		return errors.WithStack(err)
	}

	actions := make([]types.CommandFunc, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "contracts" {
			continue
		}

		actions = append(actions, RunIntegrationTests(e.Name()))
	}
	deps(actions...)
	return nil
}

// RunIntegrationTests returns function running integration tests.
func RunIntegrationTests(name string) types.CommandFunc {
	return func(ctx context.Context, deps types.DepsFunc) error {
		deps(BuildRelayerDockerImage, BuildSmartContract, tools.EnsureBridgeXRPLWASM,
			coreum.BuildCoredLocally)

		znetConfig := &infra.ConfigFactory{
			Profiles:      []string{apps.ProfileXRPLBridge},
			EnvName:       "znet",
			TimeoutCommit: 500 * time.Millisecond,
			HomeDir:       filepath.Join(lo.Must(os.UserHomeDir()), ".crust", "znet"),
			RootDir:       ".",
		}

		if err := znet.Remove(ctx, znetConfig); err != nil {
			return err
		}
		if err := znet.Start(ctx, znetConfig); err != nil {
			return err
		}
		if err := golang.RunTests(ctx, deps, golang.TestConfig{
			PackagePath: filepath.Join(testsDir, name),
			Flags: []string{
				"-timeout=30m",
				"-tags=integrationtests",
				fmt.Sprintf("-parallel=%d", 2*runtime.NumCPU()),
			},
		}); err != nil {
			return err
		}
		return znet.Remove(ctx, znetConfig)
	}
}

// RunFuzzTests runs fuzz tests.
func RunFuzzTests(ctx context.Context, deps types.DepsFunc) error {
	if err := runFuzzTest(ctx, deps, "FuzzAmountConversionCoreumToXRPLAndBack"); err != nil {
		return err
	}
	return runFuzzTest(ctx, deps, "FuzzAmountConversionCoreumToXRPLAndBack_ExceedingSignificantNumber")
}

func runFuzzTest(ctx context.Context, deps types.DepsFunc, name string) error {
	return golang.RunTests(ctx, deps, golang.TestConfig{
		PackagePath: "relayer/processes",
		Flags: []string{
			"-run", "^$",
			"-fuzz", fmt.Sprintf("^%s$", name),
			"-fuzztime", "20s",
		},
	})
}
