//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	const (
		dropSubunit         = "drop"
		xrpIssuer           = "rrrrrrrrrrrrrrrrrrrrrhoLvTp"
		xrpCurrency         = "XRP"
		xrpSendingPrecision = 6
	)

	xrpMaxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 1)

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()

	xrplBaseFee := uint32(10)
	usedTicketSequenceThreshold := uint32(10)
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		xrplBaseFee,
	)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:                    relayers,
		EvidenceThreshold:           uint32(len(relayers)),
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
		BridgeState:                 coreum.BridgeStateActive,
		XRPLBaseFee:                 xrplBaseFee,
	}, contractCfg)

	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractOwnership{
		Owner:        owner,
		PendingOwner: sdk.AccAddress{},
	}, contractOwnership)

	contractAddress := contractClient.GetContractAddress()
	tokensRes, err := assetftClient.Tokens(ctx, &assetfttypes.QueryTokensRequest{
		Issuer: contractAddress.String(),
	})
	require.NoError(t, err)
	require.Len(t, tokensRes.Tokens, 1)

	coreumDenom := fmt.Sprintf("%s-%s", dropSubunit, contractAddress.String())
	require.Equal(t, assetfttypes.Token{
		Denom:          coreumDenom,
		Issuer:         contractAddress.String(),
		Symbol:         xrpCurrency,
		Subunit:        dropSubunit,
		Precision:      6,
		Description:    "",
		GloballyFrozen: false,
		Features: []assetfttypes.Feature{
			assetfttypes.Feature_minting,
			assetfttypes.Feature_ibc,
		},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
		Admin:              contractAddress.String(),
	}, tokensRes.Tokens[0])

	// query all tokens
	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)

	require.Len(t, xrplTokens, 1)
	require.Equal(t, coreum.XRPLToken{
		Issuer:           xrpIssuer,
		Currency:         xrpCurrency,
		CoreumDenom:      coreumDenom,
		SendingPrecision: xrpSendingPrecision,
		MaxHoldingAmount: xrpMaxHoldingAmount,
		State:            coreum.TokenStateEnabled,
		BridgingFee:      sdkmath.ZeroInt(),
	}, xrplTokens[0])
}
