package bridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/coreumbridge-xrpl/build/tools"
	"github.com/CoreumFoundation/crust/build/golang"
)

// Test names.
const (
	TestContract  = "contract"
	TestProcesses = "processes"
	TestXRPL      = "xrpl"
)

// RunAllIntegrationTests runs all the bridge integration tests.
func RunAllIntegrationTests(ctx context.Context, deps build.DepsFunc) error {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		return errors.WithStack(err)
	}

	actions := make([]build.CommandFunc, 0, len(entries))
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
func RunIntegrationTests(name string) build.CommandFunc {
	return func(ctx context.Context, deps build.DepsFunc) error {
		deps(BuildSmartContract, tools.EnsureBridgeXRPLWASM)

		return golang.RunTests(ctx, deps, golang.TestConfig{
			PackagePath: filepath.Join(testsDir, name),
			Flags: []string{
				"-timeout=20m",
				"-tags=integrationtests",
			},
		})
	}
}

// RunFuzzTests runs fuzz tests.
func RunFuzzTests(ctx context.Context, deps build.DepsFunc) error {
	if err := runFuzzTest(ctx, deps, "FuzzAmountConversionCoreumToXRPLAndBack"); err != nil {
		return err
	}
	return runFuzzTest(ctx, deps, "FuzzAmountConversionCoreumToXRPLAndBack_ExceedingSignificantNumber")
}

func runFuzzTest(ctx context.Context, deps build.DepsFunc, name string) error {
	return golang.RunTests(ctx, deps, golang.TestConfig{
		PackagePath: "relayer/processes",
		Flags: []string{
			"-run", "^$",
			"-fuzz", fmt.Sprintf("^%s$", name),
			"-fuzztime", "20s",
		},
	})
}
