package bridge

import (
	"context"
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

// BuildAllIntegrationTests builds all the bridge integration tests.
func BuildAllIntegrationTests(ctx context.Context, deps build.DepsFunc) error {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		return errors.WithStack(err)
	}

	actions := make([]build.CommandFunc, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "contracts" {
			continue
		}

		actions = append(actions, BuildIntegrationTests(e.Name()))
	}
	deps(actions...)
	return nil
}

// BuildIntegrationTests returns function compiling integration tests.
func BuildIntegrationTests(name string) build.CommandFunc {
	return func(ctx context.Context, deps build.DepsFunc) error {
		deps(BuildSmartContract, tools.EnsureBridgeXRPLWASM)

		return golang.BuildTests(ctx, deps, golang.TestBuildConfig{
			PackagePath: filepath.Join(testsDir, name),
			Flags: []string{
				"-tags=integrationtests",
				binaryOutputFlag + "=" + filepath.Join(testsBinDir, repoName+"-"+name),
			},
		})
	}
}
