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

	// https://github.com/CosmWasm/wasmvm/releases
	// Check compatibility with wasmd before upgrading: https://github.com/CosmWasm/wasmd
	tools.BinaryTool{
		Name:    LibWASM,
		Version: "v1.5.2",
		Sources: tools.Sources{
			tools.TargetPlatformLinuxAMD64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v1.5.2/libwasmvm_muslc.x86_64.a",
				Hash: "sha256:e660a38efb2930b34ee6f6b0bb12730adccb040b6ab701b8f82f34453a426ae7",
				Binaries: map[string]string{
					"lib/libwasmvm_muslc.x86_64.a": "libwasmvm_muslc.x86_64.a",
				},
			},
			tools.TargetPlatformLinuxARM64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v1.5.2/libwasmvm_muslc.aarch64.a",
				Hash: "sha256:e78b224c15964817a3b75a40e59882b4d0e06fd055b39514d61646689cef8c6e",
				Binaries: map[string]string{
					"lib/libwasmvm_muslc.aarch64.a": "libwasmvm_muslc.aarch64.a",
				},
			},
			tools.TargetPlatformDarwinAMD64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v1.5.2/libwasmvmstatic_darwin.a",
				Hash: "sha256:78dd3f7c1512eca76ac9665021601ca87ee4956f1b9de9a86283d89a84bf37d4",
				Binaries: map[string]string{
					"lib/libwasmvmstatic_darwin.a": "libwasmvmstatic_darwin.a",
				},
			},
			tools.TargetPlatformDarwinARM64InDocker: {
				URL:  "https://github.com/CosmWasm/wasmvm/releases/download/v1.5.2/libwasmvmstatic_darwin.a",
				Hash: "sha256:78dd3f7c1512eca76ac9665021601ca87ee4956f1b9de9a86283d89a84bf37d4",
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
