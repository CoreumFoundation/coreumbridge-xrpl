package tools

import (
	"context"

	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

// CoreumBridgeXRPLWASMV110 is the previous version of bridge smart contract.
const (
	CoreumBridgeXRPLWASMV110 tools.Name = "coreumbridge-xrpl-wasm-v1.1.0"
	Mockgen                  tools.Name = "mockgen"
	MuslCC                   tools.Name = "muslcc"
	LibWASM                  tools.Name = "libwasmvm"
)

// Tools is a list of tools required by the bridge builder.
var Tools = []tools.Tool{
	// https://github.com/CoreumFoundation/coreumbridge-xrpl/releases
	tools.BinaryTool{
		Name:    CoreumBridgeXRPLWASMV110,
		Version: "v1.1.0",
		Local:   true,
		Sources: tools.Sources{
			tools.TargetPlatformLocal: {
				URL:  "https://github.com/CoreumFoundation/coreumbridge-xrpl/releases/download/v1.1.0/coreumbridge_xrpl.wasm",
				Hash: "sha256:9e458f31599f20a8c608056ca89ed82cc00f97c8d2ff415dd83fb95389e3e32f",
				Binaries: map[string]string{
					"bin/coreumbridge-xrpl-v1.1.0.wasm": "coreumbridge_xrpl.wasm",
				},
			},
		},
	},

	// https://github.com/uber-go/mock/releases
	tools.GoPackageTool{
		Name:    Mockgen,
		Version: "v0.4.0",
		Package: "go.uber.org/mock/mockgen",
	},

	// http://musl.cc/#binaries
	tools.BinaryTool{
		Name: MuslCC,
		// update GCP bin source when update the version
		Version: "11.2.1",
		Sources: tools.Sources{
			tools.TargetPlatformLinuxAMD64InDocker: {
				URL:  "https://storage.googleapis.com/cored-build-process-binaries/muslcc/11.2.1/x86_64-linux-musl-cross.tgz", //nolint:lll // breaking down urls is not beneficial
				Hash: "sha256:c5d410d9f82a4f24c549fe5d24f988f85b2679b452413a9f7e5f7b956f2fe7ea",
				Binaries: map[string]string{
					"bin/x86_64-linux-musl-gcc": "x86_64-linux-musl-cross/bin/x86_64-linux-musl-gcc",
				},
			},
			tools.TargetPlatformLinuxARM64InDocker: {
				URL:  "https://storage.googleapis.com/cored-build-process-binaries/muslcc/11.2.1/aarch64-linux-musl-cross.tgz", //nolint:lll // breaking down urls is not beneficial
				Hash: "sha256:c909817856d6ceda86aa510894fa3527eac7989f0ef6e87b5721c58737a06c38",
				Binaries: map[string]string{
					"bin/aarch64-linux-musl-gcc": "aarch64-linux-musl-cross/bin/aarch64-linux-musl-gcc",
				},
			},
		},
	},

	// https://github.com/CosmWasm/wasmvm/releases
	// Check compatibility with wasmd before upgrading: https://github.com/CosmWasm/wasmd
	tools.BinaryTool{
		Name:    LibWASM,
		Version: "v2.2.1",
		Sources: tools.Sources{
			tools.TargetPlatformLinuxAMD64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v2.2.1/libwasmvm_muslc.x86_64.a",
				Hash: "sha256:b3bd755efac0ff39c01b59b8110f961c48aa3eb93588071d7a628270cc1f2326",
				Binaries: map[string]string{
					"lib/libwasmvm_muslc.x86_64.a": "libwasmvm_muslc.x86_64.a",
				},
			},
			tools.TargetPlatformLinuxARM64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v2.2.1/libwasmvm_muslc.aarch64.a",
				Hash: "sha256:ba6cb5db6b14a265c8556326c045880908db9b1d2ffb5d4aa9f09ac09b24cecc",
				Binaries: map[string]string{
					"lib/libwasmvm_muslc.aarch64.a": "libwasmvm_muslc.aarch64.a",
				},
			},
			tools.TargetPlatformDarwinAMD64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v2.2.1/libwasmvmstatic_darwin.a",
				Hash: "sha256:7d732a0728b2a13b27f93cafc8c13ac5386f5b1d51e49400cddc477644fa4e47",
				Binaries: map[string]string{
					"lib/libwasmvmstatic_darwin.a": "libwasmvmstatic_darwin.a",
				},
			},
			tools.TargetPlatformDarwinARM64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v2.2.1/libwasmvmstatic_darwin.a",
				Hash: "sha256:7d732a0728b2a13b27f93cafc8c13ac5386f5b1d51e49400cddc477644fa4e47",
				Binaries: map[string]string{
					"lib/libwasmvmstatic_darwin.a": "libwasmvmstatic_darwin.a",
				},
			},
		},
	},
}

// EnsureBridgeXRPLWASM ensures bridge smart contract is available.
func EnsureBridgeXRPLWASM(ctx context.Context, _ types.DepsFunc) error {
	return tools.Ensure(ctx, CoreumBridgeXRPLWASMV110, tools.TargetPlatformLocal)
}

// EnsureMockgen ensures that mockgen is available.
func EnsureMockgen(ctx context.Context, deps types.DepsFunc) error {
	return tools.Ensure(ctx, Mockgen, tools.TargetPlatformLocal)
}
