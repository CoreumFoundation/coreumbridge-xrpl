package bridge

import (
	"context"
	"path/filepath"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/coreumbridge-xrpl/build/bridge/image"
	"github.com/CoreumFoundation/crust/build/docker"
	"github.com/CoreumFoundation/crust/build/tools"
)

type imageConfig struct {
	TargetPlatforms []tools.TargetPlatform
	Action          docker.Action
	Username        string
	Versions        []string
}

// BuildRelayerDockerImage builds docker image of the relayer.
func BuildRelayerDockerImage(ctx context.Context, deps build.DepsFunc) error {
	deps(BuildRelayer)

	return buildRelayerDockerImage(ctx, imageConfig{
		TargetPlatforms: []tools.TargetPlatform{tools.TargetPlatformLinuxLocalArchInDocker},
		Action:          docker.ActionLoad,
		Versions:        []string{"local"},
	})
}

func buildRelayerDockerImage(ctx context.Context, cfg imageConfig) error {
	dockerfile, err := image.Execute(image.Data{
		From:   docker.AlpineImage,
		Binary: binaryPath,
	})
	if err != nil {
		return err
	}

	return docker.BuildImage(ctx, docker.BuildImageConfig{
		RepoPath:        repoPath,
		ContextDir:      filepath.Join("bin", ".cache", binaryName),
		ImageName:       binaryName,
		TargetPlatforms: cfg.TargetPlatforms,
		Action:          cfg.Action,
		Versions:        cfg.Versions,
		Username:        cfg.Username,
		Dockerfile:      dockerfile,
	})
}
