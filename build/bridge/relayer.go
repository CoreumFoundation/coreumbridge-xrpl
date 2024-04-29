package bridge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/crust/build/git"
	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/tools"
)

const (
	repoPath         = "."
	binaryName       = "coreumbridge-xrpl"
	binaryPath       = "bin/" + binaryName
	testsDir         = repoPath + "/integration-tests"
	goCoverFlag      = "-cover"
	binaryOutputFlag = "-o"
)

// BuildRelayer builds all the versions of relayer binary.
func BuildRelayer(ctx context.Context, deps build.DepsFunc) error {
	deps(BuildRelayerLocally, BuildRelayerInDocker)
	return nil
}

// BuildRelayerLocally builds relayer locally.
func BuildRelayerLocally(ctx context.Context, deps build.DepsFunc) error {
	versionFlags, err := relayerVersionLDFlags(ctx)
	if err != nil {
		return err
	}

	return golang.Build(ctx, deps, golang.BinaryBuildConfig{
		TargetPlatform: tools.TargetPlatformLocal,
		PackagePath:    filepath.Join(repoPath, "relayer/cmd"),
		BinOutputPath:  binaryPath,
		Flags: []string{
			versionFlags,
		},
	})
}

// BuildRelayerInDocker builds relayer in docker.
func BuildRelayerInDocker(ctx context.Context, deps build.DepsFunc) error {
	return buildRelayerInDocker(ctx, deps, tools.TargetPlatformLinuxLocalArchInDocker, []string{goCoverFlag})
}

func buildRelayerInDocker(
	ctx context.Context,
	deps build.DepsFunc,
	targetPlatform tools.TargetPlatform,
	extraFlags []string,
) error {
	versionFlags, err := relayerVersionLDFlags(ctx)
	if err != nil {
		return err
	}

	return golang.Build(ctx, deps, golang.BinaryBuildConfig{
		TargetPlatform: targetPlatform,
		PackagePath:    filepath.Join(repoPath, "relayer/cmd"),
		BinOutputPath:  filepath.Join("bin", ".cache", binaryName, targetPlatform.String(), "bin", binaryName),
		Flags: append(extraFlags,
			versionFlags,
		),
	})
}

// Tidy runs `go mod tidy` for bridge repo.
func Tidy(ctx context.Context, deps build.DepsFunc) error {
	return golang.Tidy(ctx, repoPath, deps)
}

// Lint lints bridge repo.
func Lint(ctx context.Context, deps build.DepsFunc) error {
	deps(Generate)
	return golang.Lint(ctx, repoPath, deps)
}

// Test run unit tests in bridge repo.
func Test(ctx context.Context, deps build.DepsFunc) error {
	return golang.Test(ctx, repoPath, deps)
}

// DownloadDependencies downloads go dependencies.
func DownloadDependencies(ctx context.Context, deps build.DepsFunc) error {
	return golang.DownloadDependencies(ctx, repoPath, deps)
}

func relayerVersionLDFlags(ctx context.Context) (string, error) {
	hash, err := git.DirtyHeadHash(ctx, repoPath)
	if err != nil {
		return "", err
	}

	version, err := git.VersionFromTag(ctx, repoPath)
	if err != nil {
		return "", err
	}
	if version == "" {
		version = hash
	}

	ps := map[string]string{
		"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.VersionTag": version,
		"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo.GitCommit":  hash,
	}

	var ldFlags []string
	for k, v := range ps {
		ldFlags = append(ldFlags, fmt.Sprintf("-X %s=%s", k, v))
	}

	return "-ldflags=" + strings.Join(ldFlags, " "), nil
}
