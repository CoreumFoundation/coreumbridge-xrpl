//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"fmt"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v3/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v3/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	xrp           = "XRP"
	drop          = "drop"
	xrplPrecision = 15
	xrpIssuer     = "rrrrrrrrrrrrrrrrrrrrrho"
	xrpCurrency   = "XRP"

	eventAttributeThresholdReached = "threshold_reached"
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn",
			XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL1",
		},
	}

	usedTicketsThreshold := 10
	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:             relayers,
		EvidenceThreshold:    len(relayers),
		UsedTicketsThreshold: usedTicketsThreshold,
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
		Issuer:      xrpIssuer,
		Currency:    xrpCurrency,
		CoreumDenom: coreumDenom,
	}, xrplTokens[0])
}

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn",
			XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL1",
		},
	}

	usedTicketsThreshold := 10

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)

	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	require.Equal(t, owner.String(), contractOwnership.Owner.String())

	newOwner := chains.Coreum.GenAccount()
	// fund to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, newOwner, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
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
	_, err = contractClient.AcceptOwnership(ctx, newOwner)
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
	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn",
			XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL1",
		},
	}
	usedTicketsThreshold := 10

	notOwner := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)

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

	// try to use the registered denom with new XRPL currency on the XRPL chain
	issuerAcc := chains.XRPL.GenAccount(ctx, t, 10)
	recipientAcc := chains.XRPL.GenAccount(ctx, t, 10)

	// allow to receive the currency
	currency, err := xrpl.StringToHexXRPLCurrency(registeredToken.XRPLCurrency)
	require.NoError(t, err)
	amountToSend, err := rippledata.NewValue("10000000000000000", false)
	require.NoError(t, err)
	trustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &trustSetTx, recipientAcc))

	paymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	balancesBeforeSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipinet account balances before send: %s", balancesBeforeSend)
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &paymentTx, issuerAcc))
	balancesAfterSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipinet account balances after send: %s", balancesAfterSend)
	receiveAmount, ok := balancesAfterSend[fmt.Sprintf("%s/%s", currency.String(), issuerAcc.String())]
	require.True(t, ok)
	require.Equal(t, amountToSend.String(), receiveAmount.Value.String())
}

func TestRegisterXRPLToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn",
			XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL1",
		},
	}
	usedTicketsThreshold := 10

	notOwner := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)

	// fund owner to cover registration fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRR"
	sendingPrecision := uint32(15)
	maxHoldingAmount := "10000"

	// try to register from not owner
	_, err := contractClient.RegisterXRPLToken(ctx, notOwner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsNotOwnerError(err))

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsXRPLTokenAlreadyRegisteredError(err))

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// one XRP token and registered
	require.Len(t, xrplTokens, 2)

	var registeredToken coreum.XRPLToken
	for _, token := range xrplTokens {
		if token.Issuer == issuer && token.Currency == currency {
			registeredToken = token
			break
		}
	}
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// check that corresponding token is issued
	contractAddress := contractClient.GetContractAddress()

	tokenRes, err := assetftClient.Token(ctx, &assetfttypes.QueryTokenRequest{
		Denom: registeredToken.CoreumDenom,
	})
	require.NoError(t, err)

	// deconstruct the denom to get prefix used for the symbol and subunit
	prefix, _, err := assetfttypes.DeconstructDenom(registeredToken.CoreumDenom)
	require.NoError(t, err)

	require.Equal(t, assetfttypes.Token{
		Denom:              registeredToken.CoreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             strings.ToUpper(prefix),
		Subunit:            prefix,
		Precision:          xrplPrecision,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokenRes.Token)
}

func TestSendFromXRPLToCoreumXRPLNativeToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayer1 := coreum.Relayer{
		CoreumAddress: chains.Coreum.GenAccount(),
		XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpn",
		XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL1",
	}

	relayer2 := coreum.Relayer{
		CoreumAddress: chains.Coreum.GenAccount(),
		XRPLAddress:   "rf1BiGeXwwQoi8Z2ueFYTEXSwuJYfV2Jpx",
		XRPLPubKey:    "aBRNH5wUurfhZcoyR6nRwDSa95gMBkovBJ8V4cp1C1pM28H7EPL2",
	}

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []coreum.Relayer{
		relayer1,
		relayer2,
	}

	chains.Coreum.FundAccountWithOptions(ctx, t, relayer1.CoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})
	chains.Coreum.FundAccountWithOptions(ctx, t, relayer2.CoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})
	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	usedTicketsThreshold := 10

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund owner to cover registration fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRR"
	sendingPrecision := uint32(15)
	maxHoldingAmount := "10000"

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// find registered token
	var registeredToken coreum.XRPLToken
	for _, token := range xrplTokens {
		if token.Issuer == issuer && token.Currency == currency {
			registeredToken = token
			break
		}
	}
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// create an evidence
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    "65DEE3E51083CF037A7ED413A49DD7357964923F8CC3E3D35A24019FB771475D",
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}

	// try to call from not relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, randomAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err))

	// try use not registered token
	wrongXRPLToCoreumTransferEvidence := xrplToCoreumTransferEvidence
	wrongXRPLToCoreumTransferEvidence.Currency = "NEZ"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer1.CoreumAddress, wrongXRPLToCoreumTransferEvidence)
	require.True(t, coreum.IsTokenNotRegisteredError(err))

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer1.CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer1.CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err))

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer2.CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer1.CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err))
}
