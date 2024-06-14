package bridge

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/CoreumFoundation/crust/build/git"
	"github.com/CoreumFoundation/crust/build/golang"
	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

const (
	repoPath    = "."
	binaryName  = "coreumbridge-xrpl-relayer"
	binaryPath  = "bin/" + binaryName
	testsDir    = repoPath + "/integration-tests"
	goCoverFlag = "-cover"
)

// BuildRelayer builds all the versions of relayer binary.
func BuildRelayer(ctx context.Context, deps types.DepsFunc) error {
	deps(BuildRelayerLocally, BuildRelayerInDocker)
	return nil
}

// BuildRelayerLocally builds relayer locally.
func BuildRelayerLocally(ctx context.Context, deps types.DepsFunc) error {
	versionFlags, err := relayerVersionLDFlags(ctx)
	if err != nil {
		return err
	}

	return golang.Build(ctx, deps, golang.BinaryBuildConfig{
		TargetPlatform: tools.TargetPlatformLocal,
		PackagePath:    filepath.Join(repoPath, "relayer/cmd"),
		BinOutputPath:  binaryPath,
		LDFlags:        versionFlags,
	})
}

// BuildRelayerInDocker builds relayer in docker.
func BuildRelayerInDocker(ctx context.Context, deps types.DepsFunc) error {
	return buildRelayerInDocker(ctx, deps, tools.TargetPlatformLinuxLocalArchInDocker, []string{goCoverFlag})
}

func buildRelayerInDocker(
	ctx context.Context,
	deps types.DepsFunc,
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
		LDFlags:        versionFlags,
		Flags:          extraFlags,
	})
}

// Lint lints bridge repo.
func Lint(ctx context.Context, deps types.DepsFunc) error {
	deps(Generate, golang.Lint)
	return nil
}

// DownloadDependencies downloads go dependencies.
func DownloadDependencies(ctx context.Context, deps types.DepsFunc) error {
	return golang.DownloadDependencies(ctx, deps, repoPath)
}

func relayerVersionLDFlags(ctx context.Context) ([]string, error) {
	hash, err := git.DirtyHeadHash(ctx)
	if err != nil {
		return nil, err
	}

	version, err := git.VersionFromTag(ctx)
	if err != nil {
		return nil, err
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

	return ldFlags, nil
}
