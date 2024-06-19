package bridge

import (
	"context"

	"github.com/pkg/errors"

	"github.com/CoreumFoundation/crust/build/config"
	"github.com/CoreumFoundation/crust/build/docker"
	"github.com/CoreumFoundation/crust/build/git"
	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

// ReleaseRelayer releases relayer binary for amd64 and arm64 to be published inside the release.
func ReleaseRelayer(ctx context.Context, deps types.DepsFunc) error {
	clean, _, err := git.StatusClean(ctx)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("released commit contains uncommitted changes")
	}

	version, err := git.VersionFromTag(ctx)
	if err != nil {
		return err
	}
	if version == "" {
		return errors.New("no version present on released commit")
	}

	if err := buildRelayerInDocker(ctx, deps, tools.TargetPlatformLinuxAMD64InDocker, []string{}); err != nil {
		return err
	}
	if err := buildRelayerInDocker(ctx, deps, tools.TargetPlatformLinuxARM64InDocker, []string{}); err != nil {
		return err
	}
	if err := buildRelayerInDocker(ctx, deps, tools.TargetPlatformDarwinAMD64InDocker, []string{}); err != nil {
		return err
	}
	return buildRelayerInDocker(ctx, deps, tools.TargetPlatformDarwinARM64InDocker, []string{})
}

// ReleaseRelayerImage releases relayer docker images for amd64 and arm64.
func ReleaseRelayerImage(ctx context.Context, deps types.DepsFunc) error {
	deps(ReleaseRelayer)

	return buildRelayerDockerImage(ctx, imageConfig{
		TargetPlatforms: []tools.TargetPlatform{
			tools.TargetPlatformLinuxAMD64InDocker,
			tools.TargetPlatformLinuxARM64InDocker,
		},
		Action:   docker.ActionPush,
		Username: config.DockerHubUsername,
	})
}
