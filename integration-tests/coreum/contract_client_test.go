//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v3/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

const (
	compiledContractFilePath = "../../contract/artifacts/coreumbridge_xrpl.wasm"
	xrp                      = "XRP"
	drop                     = "drop"
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers))

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:          relayers,
		EvidenceThreshold: len(relayers),
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

	coreumDenom := fmt.Sprintf("%s-%s", drop, contractAddress.String())
	require.Equal(t, assetfttypes.Token{
		Denom:              coreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             xrp,
		Subunit:            drop,
		Precision:          6,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokensRes.Tokens[0])

	// query all tokens
	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)

	require.Len(t, xrplTokens, 1)
	require.Equal(t, coreum.XRPLToken{
		Issuer:      "",
		Currency:    "",
		CoreumDenom: coreumDenom,
	}, xrplTokens[0])
}

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers))
	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	require.Equal(t, owner.String(), contractOwnership.Owner.String())

	newOwner := chains.Coreum.GenAccount()
	// fund to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, newOwner, coreumintegration.BalancesOptions{
		Amount: sdk.NewIntFromUint64(1_000_000),
	})

	// transfer ownership
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the owner is still old until new owner accepts the ownership
	require.Equal(t, owner.String(), contractOwnership.Owner.String())
	require.Equal(t, newOwner.String(), contractOwnership.PendingOwner.String())

	// accept the ownership
	_, err = contractClient.AcceptOwnership(ctx, newOwner, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the owner is still old until new owner accepts the ownership
	require.Equal(t, newOwner.String(), contractOwnership.Owner.String())

	// try to update the ownership one more time (from old owner)
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.True(t, coreum.IsNotOwnerError(err))
}

func TestRegisterCoreumToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}

	notOwner := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: sdk.NewInt(1_000_000),
	})

	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers))

	denom1 := "denom1"
	denom1Decimals := uint32(17)

	// try to register from not owner
	_, err := contractClient.RegisterCoreumToken(ctx, notOwner, denom1, denom1Decimals)
	require.True(t, coreum.IsNotOwnerError(err))

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err))

	coreumTokens, err := contractClient.GetCoreumTokens(ctx)
	require.NoError(t, err)
	require.Len(t, coreumTokens, 1)

	registeredToken := coreumTokens[0]
	require.Equal(t, denom1, registeredToken.Denom)
	require.Equal(t, denom1Decimals, registeredToken.Decimals)
	require.NotEmpty(t, registeredToken.XRPLCurrency)
}

func deployAndInstantiateContract(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	relayers []sdk.AccAddress,
	evidenceThreshold int,
) (sdk.AccAddress, *coreum.ContractClient) {
	t.Helper()

	t.Log("Deploying and instantiating contract")
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	owner := chains.Coreum.GenAccount()

	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(10_000_000),
	})

	contractClient := coreum.NewContractClient(coreum.DefaultContractClientConfig(sdk.AccAddress(nil)), chains.Log, chains.Coreum.ClientContext)
	instantiationCfg := coreum.InstantiationConfig{
		Owner:             owner,
		Admin:             owner,
		Relayers:          relayers,
		EvidenceThreshold: evidenceThreshold,
	}
	contractAddress, err := contractClient.DeployAndInstantiate(ctx, owner, readBuiltContract(t), instantiationCfg)
	require.NoError(t, err)

	require.NoError(t, contractClient.SetContractAddress(contractAddress))

	return owner, contractClient
}

func readBuiltContract(t *testing.T) []byte {
	t.Helper()

	body, err := os.ReadFile(compiledContractFilePath)
	require.NoError(t, err)

	return body
}
