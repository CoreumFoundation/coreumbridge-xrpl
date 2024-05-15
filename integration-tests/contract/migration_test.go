//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"

	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestContractMigration(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	relayers := genRelayers(ctx, t, chains, 2)

	wasmClient := wasmtypes.NewQueryClient(chains.Coreum.ClientContext)

	xrplBridgeAddress := xrpl.GenPrivKeyTxSigner().Account()
	xrplBaseFee := uint32(10)
	owner, contractClient := integrationtests.DeployAndInstantiateContractV110(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		5,
		defaultTrustSetLimitAmount,
		xrplBridgeAddress.String(),
		xrplBaseFee,
	)

	contractInfoBeforeMigration, err := wasmClient.ContractInfo(ctx, &wasmtypes.QueryContractInfoRequest{
		Address: contractClient.GetContractAddress().String(),
	})
	require.NoError(t, err)

	integrationtests.MigrateContract(ctx, t, chains, contractClient, owner)

	contractInfoAfterMigration, err := wasmClient.ContractInfo(ctx, &wasmtypes.QueryContractInfoRequest{
		Address: contractClient.GetContractAddress().String(),
	})
	require.NoError(t, err)
	require.NotEqual(t, contractInfoBeforeMigration.CodeID, contractInfoAfterMigration.CodeID)
}
