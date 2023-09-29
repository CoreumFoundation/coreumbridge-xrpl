//go:build integrationtests
// +build integrationtests

package xrpl_test

import (
	"context"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
)

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Issuer account: %s", issuerAcc)

	recipientAcc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientAcc)

	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, issuerAcc))

	// allow the FOO coin issued by the issuer to be received by the recipient
	const fooCurrencyCode = "FOO"
	fooCurrency, err := rippledata.NewCurrency(fooCurrencyCode)
	require.NoError(t, err)
	fooCurrencyTrustSetValue, err := rippledata.NewValue("10000000000000000", false)
	require.NoError(t, err)
	fooCurrencyTrustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    fooCurrencyTrustSetValue,
			Currency: fooCurrency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooCurrencyTrustSetTx, recipientAcc))

	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	fooPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientAcc))
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooPaymentTx, issuerAcc))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientAcc))
}

func TestMultisigPayment(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	multisigAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigAcc)

	signer1Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer1 account: %s", signer1Acc)

	signer2Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer2 account: %s", signer2Acc)

	signer3Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer3 account: %s", signer3Acc)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer2Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer3Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigAcc))
	t.Logf("The signers set is updated")

	xrplPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAcc, signer1Acc)
	signer1 := chains.XRPL.Multisign(t, &xrplPaymentTx, signer1Acc)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAcc, signer1Acc)
	signer2 := chains.XRPL.Multisign(t, &xrplPaymentTx, signer2Acc)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAcc, signer1Acc)
	signer3 := chains.XRPL.Multisign(t, &xrplPaymentTx, signer3Acc)

	xrpPaymentTxTwoSigners := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAcc, signer1Acc)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxTwoSigners, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	xrpPaymentTxThreeSigners := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAcc, signer1Acc)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxThreeSigners, []rippledata.Signer{
		signer1,
		signer2,
		signer3,
	}...))

	// compare hashes
	t.Logf("TwoSignersHash/ThreeSignersHash: %s/%s", xrpPaymentTxTwoSigners.Hash, xrpPaymentTxThreeSigners.Hash)
	require.NotEqual(t, xrpPaymentTxTwoSigners.Hash.String(), xrpPaymentTxThreeSigners.Hash.String())

	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTxTwoSigners.Destination))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &xrpPaymentTxTwoSigners))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTxTwoSigners.Destination))

	// try to submit with three signers (the transaction won't be accepted)
	require.ErrorContains(t, chains.XRPL.SubmitTx(ctx, t, &xrpPaymentTxThreeSigners), "This sequence number has already passed")
}

func TestCreateAndUseTicketForPaymentAndTicketsCreation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	senderAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Sender account: %s", senderAcc)

	recipientAcc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientAcc)

	ticketsToCreate := 1
	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &createTicketsTx, senderAcc))
	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := integrationtests.ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// create tickets with ticket
	ticketsToCreate = 2
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &createTicketsTx, senderAcc)
	// reset sequence and add ticket
	createTicketsTx.TxBase.Sequence = 0
	createTicketsTx.TicketSequence = createdTickets[0].TicketSequence
	require.NoError(t, chains.XRPL.SignAndSubmitTx(ctx, t, &createTicketsTx, senderAcc))

	txRes, err = chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = integrationtests.ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// send XRP coins from sender to recipient with ticket
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &xrpPaymentTx, senderAcc)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = createdTickets[0].TicketSequence

	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientAcc))
	require.NoError(t, chains.XRPL.SignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientAcc))

	// try to use tickets for the transactions without the trust-line
	const newFooCurrencyCode = "NFO"
	fooCurrency, err := rippledata.NewCurrency(newFooCurrencyCode)
	require.NoError(t, err)
	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	ticketForFailingTx := createdTickets[1].TicketSequence
	fooPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   senderAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &fooPaymentTx, senderAcc)
	// reset sequence and add ticket
	fooPaymentTx.TxBase.Sequence = 0
	fooPaymentTx.TicketSequence = ticketForFailingTx
	// there is no trust set so the tx should fail and use the ticket
	require.ErrorContains(t, chains.XRPL.SignAndSubmitTx(ctx, t, &fooPaymentTx, senderAcc), "Path could not send partial amount")

	// try to reuse the ticket for the success tx
	xrpPaymentTx = rippledata.Payment{
		Destination: recipientAcc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &xrpPaymentTx, senderAcc)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = ticketForFailingTx
	// the ticket is used in prev failed transaction so can't be used here
	require.ErrorContains(t, chains.XRPL.SignAndSubmitTx(ctx, t, &fooPaymentTx, senderAcc), "Ticket is not in ledger")
}

func TestCreateAndUseTicketForTicketsCreationWithMultisigning(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	multisigAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigAcc)

	signer1Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer1 account: %s", signer1Acc)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigAcc))
	t.Logf("The signers set is updated")

	ticketsToCreate := uint32(1)
	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigAcc)
	signer1 := chains.XRPL.Multisign(t, &createTicketsTx, signer1Acc)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := integrationtests.ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, createdTickets[0].TicketSequence, multisigAcc)
	signer1 = chains.XRPL.Multisign(t, &createTicketsTx, signer1Acc)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, createdTickets[0].TicketSequence, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err = chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = integrationtests.ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))
}

func TestCreateAndUseTicketForMultisigningKeysRotation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	multisigAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigAcc)

	signer1Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer1 account: %s", signer1Acc)

	signer2Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer2 account: %s", signer2Acc)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigAcc))

	ticketsToCreate := uint32(2)

	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigAcc)
	signer1 := chains.XRPL.Multisign(t, &createTicketsTx, signer1Acc)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := integrationtests.ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	updateSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer2Acc, createdTickets[0].TicketSequence, multisigAcc)
	signer1 = chains.XRPL.Multisign(t, &updateSignerListSetTx, signer1Acc)

	updateSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer2Acc, createdTickets[0].TicketSequence, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&updateSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &updateSignerListSetTx))

	// try to sign and send with previous signer
	restoreSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer1Acc, createdTickets[1].TicketSequence, multisigAcc)
	signer1 = chains.XRPL.Multisign(t, &restoreSignerListSetTx, signer1Acc)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer1Acc, createdTickets[1].TicketSequence, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.ErrorContains(t, chains.XRPL.SubmitTx(ctx, t, &restoreSignerListSetTx), "A signature is provided for a non-signer")

	// build and send with correct signer
	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer1Acc, createdTickets[1].TicketSequence, multisigAcc)
	signer2 := chains.XRPL.Multisign(t, &restoreSignerListSetTx, signer2Acc)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, signer1Acc, createdTickets[1].TicketSequence, multisigAcc)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer2,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &restoreSignerListSetTx))
}

func TestMultisigWithMasterKeyRemoval(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	multisigAccToDisable := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigAccToDisable)

	signer1Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer1 account: %s", signer1Acc)

	signer2Acc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Signer2 account: %s", signer2Acc)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer2Acc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigAccToDisable))
	t.Logf("The signers set is updated")

	// disable master key now to be able to use multi-signing only
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         multisigAccToDisable,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(4)),
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &disableMasterKeyTx, multisigAccToDisable))
	t.Logf("The master key is disabled")

	// try to update signers one more time
	require.ErrorContains(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigAccToDisable), "Master key is disabled")

	// now use multi-signing for the account
	xrpPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAccToDisable, signer1Acc)
	signer1 := chains.XRPL.Multisign(t, &xrpPaymentTx, signer1Acc)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAccToDisable, signer1Acc)
	signer2 := chains.XRPL.Multisign(t, &xrpPaymentTx, signer2Acc)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigAccToDisable, signer1Acc)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTx.Destination))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &xrpPaymentTx))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTx.Destination))
}

func TestLedgerCurrent(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	currentLedger, err := chains.XRPL.RPCClient().LedgerCurrent(ctx)
	require.NoError(t, err)
	require.Greater(t, currentLedger.LedgerCurrentIndex, int64(0))
}

func TestAccountTx(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	senderAcc := chains.XRPL.GenAccount(ctx, t, 10)
	t.Logf("Sender account: %s", senderAcc)

	recipientAcc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientAcc)

	// send 4 txs from is the sender to the recipient
	for i := 0; i < 4; i++ {
		xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
		require.NoError(t, err)
		xrpPaymentTx := rippledata.Payment{
			Destination: recipientAcc,
			Amount:      *xrpAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
			},
		}
		require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
	}

	accountTxRes, err := chains.XRPL.RPCClient().AccountTx(ctx, senderAcc, -1, -1, nil)
	require.NoError(t, err)
	require.Len(t, accountTxRes.Transactions, 5) // faucet send + 4 more
}

func buildXrpPaymentTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	from, to rippledata.Account,
) rippledata.Payment {
	t.Helper()

	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)

	tx := rippledata.Payment{
		Destination: to,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	xrplChain.AutoFillTx(ctx, t, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return tx
}

func buildCreateTicketsTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	ticketsToCreate uint32,
	ticketSeq *uint32,
	from rippledata.Account,
) rippledata.TicketCreate {
	t.Helper()

	tx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(ticketsToCreate),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	xrplChain.AutoFillTx(ctx, t, &tx, from)

	if ticketSeq != nil {
		tx.Sequence = 0
		tx.TicketSequence = ticketSeq
	}
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return tx
}

func buildUpdateSignerListSetTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	signerAcc rippledata.Account,
	ticketSeq *uint32,
	from rippledata.Account,
) rippledata.SignerListSet {
	t.Helper()

	tx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signerAcc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	xrplChain.AutoFillTx(ctx, t, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	if ticketSeq != nil {
		tx.Sequence = 0
		tx.TicketSequence = ticketSeq
	}

	return tx
}
