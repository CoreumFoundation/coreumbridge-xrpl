package tools

import (
	"context"

	"github.com/CoreumFoundation/crust/build/tools"
	"github.com/CoreumFoundation/crust/build/types"
)

const (
	// CoreumBridgeXRPLWASMV117 is the previous version of bridge smart contract.
	CoreumBridgeXRPLWASMV117 tools.Name = "coreumbridge-xrpl-wasm-v1.1.7"

	// Mockgen is used to generate mock files.
	Mockgen tools.Name = "mockgen"
)

// Tools is a list of tools required by the bridge builder.
var Tools = []tools.Tool{
	// https://github.com/CoreumFoundation/coreumbridge-xrpl/releases
	tools.BinaryTool{
		Name:    CoreumBridgeXRPLWASMV117,
		Version: "v1.1.7",
		Local:   true,
		Sources: tools.Sources{
			tools.TargetPlatformLocal: {
				URL:  "https://github.com/CoreumFoundation/coreumbridge-xrpl/releases/download/v1.1.7/coreumbridge_xrpl.wasm",
				Hash: "sha256:02a4cdb98ee891664ee5081209a1b55a7c7e3e4b01455009f4466b3a280de6dc",
				Binaries: map[string]string{
					"bin/coreumbridge-xrpl-v1.1.7.wasm": "coreumbridge_xrpl.wasm",
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
}

// EnsureBridgeXRPLWASM ensures bridge smart contract is available.
func EnsureBridgeXRPLWASM(ctx context.Context, _ types.DepsFunc) error {
	return tools.Ensure(ctx, CoreumBridgeXRPLWASMV117, tools.TargetPlatformLocal)
}

// EnsureMockgen ensures that mockgen is available.
func EnsureMockgen(ctx context.Context, deps types.DepsFunc) error {
	return tools.Ensure(ctx, Mockgen, tools.TargetPlatformLocal)
}
