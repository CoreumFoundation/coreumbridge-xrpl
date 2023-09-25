//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"context"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)

	issuerWallet := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Issuer account: %s", issuerWallet.Account)

	recipientWallet := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	// send XRP coins from issuer to recipient (if account is new you need to send 10 XRP to activate it)
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, issuerWallet))

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
			Issuer:   issuerWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooCurrencyTrustSetTx, recipientWallet))

	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	fooPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   issuerWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientWallet.Account))
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooPaymentTx, issuerWallet))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientWallet.Account))
}

func TestMultisigPayment(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)

	multisigWallet := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	wallet3 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet3 account: %s", wallet3.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet2.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet3.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	xrplPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWallet.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWallet.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWallet.Account, wallet1.Account)
	signer3, err := wallet3.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrpPaymentTxTwoSigners := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWallet.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxTwoSigners, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	xrpPaymentTxThreeSigners := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWallet.Account, wallet1.Account)
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

	ctx, chains := NewTestingContext(t)

	senderWallet := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Sender account: %s", senderWallet.Account)

	recipientWallet := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	ticketsToCreate := 1
	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &createTicketsTx, senderWallet))
	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// create tickets with ticket
	ticketsToCreate = 2
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &createTicketsTx, senderWallet.Account)
	// reset sequence and add ticket
	createTicketsTx.TxBase.Sequence = 0
	createTicketsTx.TicketSequence = createdTickets[0].TicketSequence
	require.NoError(t, chains.XRPL.SignAndSubmitTx(ctx, t, &createTicketsTx, senderWallet))

	txRes, err = chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// send XRP coins from sender to recipient with ticket
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = createdTickets[0].TicketSequence

	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientWallet.Account))
	require.NoError(t, chains.XRPL.SignAndSubmitTx(ctx, t, &xrpPaymentTx, senderWallet))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, recipientWallet.Account))

	// try to use tickets for the transactions without the trust-line
	const newFooCurrencyCode = "NFO"
	fooCurrency, err := rippledata.NewCurrency(newFooCurrencyCode)
	require.NoError(t, err)
	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	ticketForFailingTx := createdTickets[1].TicketSequence
	fooPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   senderWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &fooPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	fooPaymentTx.TxBase.Sequence = 0
	fooPaymentTx.TicketSequence = ticketForFailingTx
	// there is no trust set so the tx should fail and use the ticket
	require.ErrorContains(t, chains.XRPL.SignAndSubmitTx(ctx, t, &fooPaymentTx, senderWallet), "Path could not send partial amount")

	// try to reuse the ticket for the success tx
	xrpPaymentTx = rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	chains.XRPL.AutoFillTx(ctx, t, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = ticketForFailingTx
	// the ticket is used in prev failed transaction so can't be used here
	require.ErrorContains(t, chains.XRPL.SignAndSubmitTx(ctx, t, &fooPaymentTx, senderWallet), "Ticket is not in ledger")
}

func TestCreateAndUseTicketForTicketsCreationWithMultisigning(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)

	multisigWallet := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	ticketsToCreate := uint32(1)
	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err = chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))
}

func TestCreateAndUseTicketForMultisigningKeysRotation(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)

	multisigWallet := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigWallet))

	ticketsToCreate := uint32(2)

	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, chains.XRPL, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &createTicketsTx))

	txRes, err := chains.XRPL.RPCClient().Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := ExtractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	updateSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&updateSignerListSetTx)
	require.NoError(t, err)

	updateSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&updateSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &updateSignerListSetTx))

	// try to sign and send with previous signer
	restoreSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.ErrorContains(t, chains.XRPL.SubmitTx(ctx, t, &restoreSignerListSetTx), "A signature is provided for a non-signer")

	// build and send with correct signer
	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer2, err := wallet2.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, chains.XRPL, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer2,
	}...))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &restoreSignerListSetTx))
}

func TestMultisigWithMasterKeyRemoval(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)

	multisigWalletToDisable := chains.XRPL.GenWallet(ctx, t, 10)
	t.Logf("Multisig account: %s", multisigWalletToDisable.Account)

	wallet1 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet2.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigWalletToDisable))
	t.Logf("The signers set is updated")

	// disable master key now to be able to use multi-signing only
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         multisigWalletToDisable.Account,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(4)),
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &disableMasterKeyTx, multisigWalletToDisable))
	t.Logf("The master key is disabled")

	// try to update signers one more time
	require.ErrorContains(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &signerListSetTx, multisigWalletToDisable), "Master key is disabled")

	// now use multi-signing for the account
	xrpPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWalletToDisable.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWalletToDisable.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, chains.XRPL, multisigWalletToDisable.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	t.Logf("Recipinet account balance before: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTx.Destination))
	require.NoError(t, chains.XRPL.SubmitTx(ctx, t, &xrpPaymentTx))
	t.Logf("Recipinet account balance after: %s", chains.XRPL.GetAccountBalances(ctx, t, xrpPaymentTx.Destination))
}

func buildXrpPaymentTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	xrplChain XRPLChain,
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
	xrplChain XRPLChain,
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
	xrplChain XRPLChain,
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
