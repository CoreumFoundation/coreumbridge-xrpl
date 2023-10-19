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
	"github.com/samber/lo"
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
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
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
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
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
	require.True(t, coreum.IsNotOwnerError(err), err)
}

func TestRegisterCoreumToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
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
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err), err)

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
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
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

	// try to register from not owner
	_, err := contractClient.RegisterXRPLToken(ctx, notOwner, issuer, currency)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency)
	require.True(t, coreum.IsXRPLTokenAlreadyRegisteredError(err), err)

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
		XRPLAddress:   "xrpl_address",
		XRPLPubKey:    "xrpl_pub_key",
	}

	relayer2 := coreum.Relayer{
		CoreumAddress: chains.Coreum.GenAccount(),
		XRPLAddress:   "xrpl_address",
		XRPLPubKey:    "xrpl_pub_key",
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

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency)
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
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try use not registered token
	wrongXRPLToCoreumTransferEvidence := xrplToCoreumTransferEvidence
	wrongXRPLToCoreumTransferEvidence.Currency = "NEZ"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer1.CoreumAddress, wrongXRPLToCoreumTransferEvidence)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

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
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

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
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)
}

func TestRecoverTickets(t *testing.T) {
	t.Parallel()

	// TODO(dzmitryhil) extend the test to check multiple operations once we have them allowed to be created
	usedTicketsThreshold := 5
	numberOfTicketsToInit := uint32(4)

	ctx, chains := integrationtests.NewTestingContext(t)
	relayer1 := chains.Coreum.GenAccount()
	relayer2 := chains.Coreum.GenAccount()
	relayer3 := chains.Coreum.GenAccount()

	xrplSigner1 := chains.XRPL.GenAccount(ctx, t, 0)
	xrplSigner2 := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := []sdk.AccAddress{
		relayer1,
		relayer2,
		relayer3,
	}

	for _, relayer := range relayers {
		chains.Coreum.FundAccountWithOptions(ctx, t, relayer, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewInt(1_000_000),
		})
	}

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, 2, usedTicketsThreshold)

	// ********** Ticket allocation / Recovery **********
	firstBridgeAccountSeqNumber := uint64(1)

	// try to call from not owner
	_, err := contractClient.RecoverTickets(ctx, relayers[0], firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// try to use more than max allowed tickets
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, lo.ToPtr(uint32(251)))
	require.True(t, coreum.IsInvalidTicketNumberToAllocateError(err), err)

	// try to use zero tickets
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, lo.ToPtr(uint32(0)))
	require.True(t, coreum.IsInvalidTicketNumberToAllocateError(err), err)

	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	availableTickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// check that we have just one operation with correct data
	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation := pendingOperations[0]
	require.Equal(t, coreum.Operation{
		TicketNumber:   0,
		SequenceNumber: firstBridgeAccountSeqNumber,
		Signatures:     make([]coreum.Signature, 0),
		OperationType: coreum.OperationType{
			AllocateTickets: coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// try to recover tickets when the tickets allocation is in-process
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsPendingTicketUpdateError(err), err)

	// ********** Signatures **********

	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(numberOfTicketsToInit),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	signerItem1 := chains.XRPL.Multisign(t, &createTicketsTx, xrplSigner1).Signer
	// try to send from not relayer
	_, err = contractClient.RegisterSignature(ctx, owner, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to send with incorrect operation ID
	_, err = contractClient.RegisterSignature(ctx, relayer1, uint64(999), signerItem1.TxnSignature.String())
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// send from first relayer
	_, err = contractClient.RegisterSignature(ctx, relayer1, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
	require.NoError(t, err)

	// try to send from the same relayer one more time
	_, err = contractClient.RegisterSignature(ctx, relayer1, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
	require.True(t, coreum.IsSignatureAlreadyProvidedError(err), err)

	// send from second relayer
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(numberOfTicketsToInit),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	signerItem2 := chains.XRPL.Multisign(t, &createTicketsTx, xrplSigner2).Signer
	_, err = contractClient.RegisterSignature(ctx, relayer2, firstBridgeAccountSeqNumber, signerItem2.TxnSignature.String())
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation = pendingOperations[0]
	require.Equal(t, coreum.Operation{
		TicketNumber:   0,
		SequenceNumber: firstBridgeAccountSeqNumber,
		Signatures: []coreum.Signature{
			{
				Relayer:   relayer1.String(),
				Signature: signerItem1.TxnSignature.String(),
			},
			{
				Relayer:   relayer2.String(),
				Signature: signerItem2.TxnSignature.String(),
			},
		},
		OperationType: coreum.OperationType{
			AllocateTickets: coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// ********** TransactionResultEvidence / Transaction rejected **********

	rejectedTxHash := "FC7B3043C73998C6696C788D73621D55D7C05BEBBA0A14C186AF43F6B12AE8E3"
	rejectedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:         rejectedTxHash,
			SequenceNumber: &firstBridgeAccountSeqNumber,
			Confirmed:      false,
		},
		Tickets: nil,
	}

	// try to send with not existing sequence
	invalidRejectedTxEvidence := rejectedTxEvidence
	invalidRejectedTxEvidence.SequenceNumber = lo.ToPtr(uint64(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, invalidRejectedTxEvidence)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send with not existing ticket
	invalidRejectedTxEvidence = rejectedTxEvidence
	invalidRejectedTxEvidence.SequenceNumber = nil
	invalidRejectedTxEvidence.TicketNumber = lo.ToPtr(uint64(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, invalidRejectedTxEvidence)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send from not relayer
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, owner, rejectedTxEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// send evidence from first relayer
	txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, rejectedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReached)

	// try to send evidence from second relayer one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, rejectedTxEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer2, rejectedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	// try to send the evidence one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, rejectedTxEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 0)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// ********** Ticket allocation after previous failure / Recovery **********

	secondBridgeAccountSeqNumber := uint64(2)
	// start the process one more time
	_, err = contractClient.RecoverTickets(ctx, owner, secondBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	// ********** TransactionResultEvidence / Transaction accepted **********

	// we can omit the signing here since it is required only for the tx submission.
	acceptedTxHash := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C8"
	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:         acceptedTxHash,
			SequenceNumber: &secondBridgeAccountSeqNumber,
			Confirmed:      true,
		},
		Tickets: []uint64{3, 5, 6, 7},
	}

	// try to send with already used txHash
	invalidAcceptedTxEvidence := acceptedTxEvidence
	invalidAcceptedTxEvidence.TxHash = rejectedTxHash
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, invalidAcceptedTxEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// send evidence from first relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer1, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReached)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer2, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 0)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Equal(t, acceptedTxEvidence.Tickets, availableTickets)
}
