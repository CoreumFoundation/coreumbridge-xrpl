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
}

// EnsureBridgeXRPLWASM ensures bridge smart contract is available.
func EnsureBridgeXRPLWASM(ctx context.Context, _ types.DepsFunc) error {
	return tools.Ensure(ctx, CoreumBridgeXRPLWASMV110, tools.TargetPlatformLocal)
}

// EnsureMockgen ensures that mockgen is available.
func EnsureMockgen(ctx context.Context, deps types.DepsFunc) error {
	return tools.Ensure(ctx, Mockgen, tools.TargetPlatformLocal)
}
