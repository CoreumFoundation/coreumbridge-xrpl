//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/pkg/errors"
	ripplecrypto "github.com/rubblelabs/ripple/crypto"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client/http"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client/xrpl"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

var (
	xrpCurrency      = "XRP"
	host             = "http://localhost:5005/"
	ecdsaKeyType     = rippledata.ECDSA
	faucetSeedPhrase = "snoPBrXtMeMyMHUVTgbuqAfg1SUTb"
)

// ********** Wallet **********

type Wallet struct {
	Key      ripplecrypto.Key
	Sequence *uint32
	Account  rippledata.Account
}

func NewWalletFromSeedPhrase(seedPhrase string) (Wallet, error) {
	seed, err := rippledata.NewSeedFromAddress(seedPhrase)
	if err != nil {
		return Wallet{}, err
	}

	key := seed.Key(ecdsaKeyType)
	seq := lo.ToPtr(uint32(0))
	account := seed.AccountId(ecdsaKeyType, seq)

	return Wallet{
		Key:      key,
		Sequence: seq,
		Account:  account,
	}, nil
}

func (w Wallet) MultiSign(tx rippledata.MultiSignable) (rippledata.Signer, error) {
	if err := rippledata.MultiSign(tx, w.Key, w.Sequence, w.Account); err != nil {
		return rippledata.Signer{}, err
	}

	return rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       w.Account,
			TxnSignature:  tx.GetSignature(),
			SigningPubKey: tx.GetPublicKey(),
		},
	}, nil
}

// ********** Tests **********

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	issuerWallet := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Issuer account: %s", issuerWallet.Account)

	recipientWallet := genWallet(ctx, t, rpcClient, 0)
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

	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &xrpPaymentTx, issuerWallet))

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
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &fooCurrencyTrustSetTx, recipientWallet))

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
	t.Logf("Recipinet account balance before: %s", getAccountBalance(ctx, t, rpcClient, recipientWallet.Account))
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &fooPaymentTx, issuerWallet))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(ctx, t, rpcClient, recipientWallet.Account))
}

func TestMultisigPayment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	multisigWallet := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := genWallet(ctx, t, rpcClient, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := genWallet(ctx, t, rpcClient, 0)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	wallet3 := genWallet(ctx, t, rpcClient, 0)
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
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	xrplPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWallet.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWallet.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWallet.Account, wallet1.Account)
	signer3, err := wallet3.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrpPaymentTxTwoSigners := buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWallet.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxTwoSigners, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	xrpPaymentTxThreeSigners := buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWallet.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxThreeSigners, []rippledata.Signer{
		signer1,
		signer2,
		signer3,
	}...))

	// compare hashes
	t.Logf("TwoSignersHash/ThreeSignersHash: %s/%s", xrpPaymentTxTwoSigners.Hash, xrpPaymentTxThreeSigners.Hash)
	require.NotEqual(t, xrpPaymentTxTwoSigners.Hash.String(), xrpPaymentTxThreeSigners.Hash.String())

	t.Logf("Recipinet account balance before: %s", getAccountBalance(ctx, t, rpcClient, xrpPaymentTxTwoSigners.Destination))
	require.NoError(t, submitTx(ctx, t, rpcClient, &xrpPaymentTxTwoSigners))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(ctx, t, rpcClient, xrpPaymentTxTwoSigners.Destination))

	// try to submit with three signers (the transaction won't be accepted)
	require.ErrorContains(t, submitTx(ctx, t, rpcClient, &xrpPaymentTxThreeSigners), "This sequence number has already passed")
}

func TestCreateAndUseTicketForPaymentAndTicketsCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	senderWallet := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Sender account: %s", senderWallet.Account)

	recipientWallet := genWallet(ctx, t, rpcClient, 0)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	ticketsToCreate := 1
	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &createTicketsTx, senderWallet))
	txRes, err := rpcClient.Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// create tickets with ticket
	ticketsToCreate = 2
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	autoFillTx(ctx, t, rpcClient, &createTicketsTx, senderWallet.Account)
	// reset sequence and add ticket
	createTicketsTx.TxBase.Sequence = 0
	createTicketsTx.TicketSequence = createdTickets[0].TicketSequence
	require.NoError(t, signAndSubmitTx(ctx, t, rpcClient, &createTicketsTx, senderWallet))

	txRes, err = rpcClient.Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
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
	autoFillTx(ctx, t, rpcClient, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = createdTickets[0].TicketSequence

	t.Logf("Recipinet account balance before: %s", getAccountBalance(ctx, t, rpcClient, recipientWallet.Account))
	require.NoError(t, signAndSubmitTx(ctx, t, rpcClient, &xrpPaymentTx, senderWallet))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(ctx, t, rpcClient, recipientWallet.Account))

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
	autoFillTx(ctx, t, rpcClient, &fooPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	fooPaymentTx.TxBase.Sequence = 0
	fooPaymentTx.TicketSequence = ticketForFailingTx
	// there is no trust set so the tx should fail and use the ticket
	require.ErrorContains(t, signAndSubmitTx(ctx, t, rpcClient, &fooPaymentTx, senderWallet), "Path could not send partial amount")

	// try to reuse the ticket for the success tx
	xrpPaymentTx = rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(ctx, t, rpcClient, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = ticketForFailingTx
	// the ticket is used in prev failed transaction so can't be used here
	require.ErrorContains(t, signAndSubmitTx(ctx, t, rpcClient, &fooPaymentTx, senderWallet), "Ticket is not in ledger")
}

func TestCreateAndUseTicketForTicketsCreationWithMultisigning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	multisigWallet := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := genWallet(ctx, t, rpcClient, 0)
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
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	ticketsToCreate := uint32(1)
	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, submitTx(ctx, t, rpcClient, &createTicketsTx))

	txRes, err := rpcClient.Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, submitTx(ctx, t, rpcClient, &createTicketsTx))

	txRes, err = rpcClient.Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))
}

func TestCreateAndUseTicketForMultisigningKeysRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)
	multisigWallet := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1 := genWallet(ctx, t, rpcClient, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := genWallet(ctx, t, rpcClient, 0)
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
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &signerListSetTx, multisigWallet))

	ticketsToCreate := uint32(2)

	createTicketsTx := buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(ctx, t, rpcClient, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, submitTx(ctx, t, rpcClient, &createTicketsTx))

	txRes, err := rpcClient.Tx(ctx, *createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	updateSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&updateSignerListSetTx)
	require.NoError(t, err)

	updateSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&updateSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, submitTx(ctx, t, rpcClient, &updateSignerListSetTx))

	// try to sign and send with previous signer
	restoreSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.ErrorContains(t, submitTx(ctx, t, rpcClient, &restoreSignerListSetTx), "A signature is provided for a non-signer")

	// build and send with correct signer
	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer2, err := wallet2.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(ctx, t, rpcClient, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer2,
	}...))
	require.NoError(t, submitTx(ctx, t, rpcClient, &restoreSignerListSetTx))
}

func TestMultisigWithMasterKeyRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(host),
		logger.NewZapLogger(zaptest.NewLogger(t)),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	multisigWalletToDisable := genWallet(ctx, t, rpcClient, 10_000000)
	t.Logf("Multisig account: %s", multisigWalletToDisable.Account)

	wallet1 := genWallet(ctx, t, rpcClient, 0)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2 := genWallet(ctx, t, rpcClient, 0)
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
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &signerListSetTx, multisigWalletToDisable))
	t.Logf("The signers set is updated")

	// disable master key now to be able to use multi-signing only
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         multisigWalletToDisable.Account,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(4)),
	}
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &disableMasterKeyTx, multisigWalletToDisable))
	t.Logf("The master key is disabled")

	// try to update signers one more time
	require.ErrorContains(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &signerListSetTx, multisigWalletToDisable), "Master key is disabled")

	// now use multi-signing for the account
	xrpPaymentTx := buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWalletToDisable.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWalletToDisable.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(ctx, t, rpcClient, multisigWalletToDisable.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	t.Logf("Recipinet account balance before: %s", getAccountBalance(ctx, t, rpcClient, xrpPaymentTx.Destination))
	require.NoError(t, submitTx(ctx, t, rpcClient, &xrpPaymentTx))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(ctx, t, rpcClient, xrpPaymentTx.Destination))
}

func getAccountBalance(ctx context.Context, t *testing.T, rpcClient *xrpl.RPCClient, acc rippledata.Account) map[string]rippledata.Amount {
	t.Helper()

	amounts := make(map[string]rippledata.Amount, 0)

	accInfo, err := rpcClient.AccountInfo(ctx, acc)
	require.NoError(t, err)
	amounts[xrpCurrency] = rippledata.Amount{
		Value: accInfo.AccountData.Balance,
	}
	// none xrp amounts
	accLines, err := rpcClient.AccountLines(ctx, acc, "closed", nil)
	require.NoError(t, err)

	for _, line := range accLines.Lines {
		lineCopy := line
		amounts[fmt.Sprintf("%s/%s", lineCopy.Currency.String(), lineCopy.Account.String())] = rippledata.Amount{
			Value:    &lineCopy.Balance.Value,
			Currency: lineCopy.Currency,
			Issuer:   lineCopy.Account,
		}
	}

	return amounts
}

func autoFillSignAndSubmitTx(
	ctx context.Context,
	t *testing.T,
	rpcClient *xrpl.RPCClient,
	tx rippledata.Transaction,
	wallet Wallet,
) error {
	t.Helper()

	autoFillTx(ctx, t, rpcClient, tx, wallet.Account)
	return signAndSubmitTx(ctx, t, rpcClient, tx, wallet)
}

func signAndSubmitTx(
	ctx context.Context,
	t *testing.T,
	rpcClient *xrpl.RPCClient,
	tx rippledata.Transaction,
	wallet Wallet,
) error {
	t.Helper()

	require.NoError(t, rippledata.Sign(tx, wallet.Key, wallet.Sequence))
	return submitTx(ctx, t, rpcClient, tx)
}

func autoFillTx(ctx context.Context, t *testing.T, rpcClient *xrpl.RPCClient, tx rippledata.Transaction, sender rippledata.Account) {
	t.Helper()

	accInfo, err := rpcClient.AccountInfo(ctx, sender)
	require.NoError(t, err)
	// update base settings
	base := tx.GetBase()
	fee, err := rippledata.NewValue("100", true)
	require.NoError(t, err)
	base.Fee = *fee
	base.Account = sender
	base.Sequence = *accInfo.AccountData.Sequence
}

func submitTx(ctx context.Context, t *testing.T, rpcClient *xrpl.RPCClient, tx rippledata.Transaction) error {
	t.Helper()

	t.Logf("Submitting transaction, hash:%s", tx.GetHash())
	// submit the transaction
	res, err := rpcClient.Submit(ctx, tx)
	if err != nil {
		return err
	}
	if !res.EngineResult.Success() {
		return errors.Errorf("the tx submition is failed, %+v", res)
	}

	retryCtx, retryCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer retryCtxCancel()

	t.Logf("Transaction is submitted waitig for hash:%s", tx.GetHash())
	return retry.Do(retryCtx, 250*time.Millisecond, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(ctx, 3*time.Second)
		defer reqCtxCancel()
		txRes, err := rpcClient.Tx(reqCtx, *tx.GetHash())
		if err != nil {
			return retry.Retryable(err)
		}

		if !txRes.Validated {
			return retry.Retryable(errors.Errorf("transaction is not validated"))
		}

		return nil
	})
}

func extractTicketsFromMeta(txRes xrpl.TxResult) []*rippledata.Ticket {
	createdTickets := make([]*rippledata.Ticket, 0)
	for _, node := range txRes.MetaData.AffectedNodes {
		createdNode := node.CreatedNode
		if createdNode == nil {
			continue
		}
		newFields := createdNode.NewFields
		if newFields == nil {
			continue
		}
		if rippledata.TICKET.String() != newFields.GetType() {
			continue
		}
		ticket, ok := newFields.(*rippledata.Ticket)
		if !ok {
			continue
		}
		createdTickets = append(createdTickets, ticket)
	}

	return createdTickets
}

func genWallet(ctx context.Context, t *testing.T, rpcClient *xrpl.RPCClient, amount int64) Wallet {
	t.Helper()

	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}

	familySeed, err := ripplecrypto.GenerateFamilySeed(string(b))
	if err != nil {
		panic(err)
	}
	seed, err := rippledata.NewSeedFromAddress(familySeed.String())
	if err != nil {
		panic(err)
	}

	wallet, err := NewWalletFromSeedPhrase(seed.String())
	require.NoError(t, err)

	fundAccount(ctx, t, rpcClient, wallet.Account, amount+10_000000) // 10XRP to active

	return wallet
}

func fundAccount(ctx context.Context, t *testing.T, rpcClient *xrpl.RPCClient, acc rippledata.Account, amount int64) {
	t.Helper()

	xrpAmount, err := rippledata.NewAmount(amount)
	require.NoError(t, err)
	fundXrpTx := rippledata.Payment{
		Destination: acc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	wallet, err := NewWalletFromSeedPhrase(faucetSeedPhrase)
	require.NoError(t, err)
	t.Logf("Funding account, account address: %s, amount: %d", acc, amount)
	require.NoError(t, autoFillSignAndSubmitTx(ctx, t, rpcClient, &fundXrpTx, wallet))
	t.Logf("The account %s is funded", acc)
}

func buildXrpPaymentTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	rpcClient *xrpl.RPCClient,
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
	autoFillTx(ctx, t, rpcClient, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return tx
}

func buildCreateTicketsTxForMultiSigning(
	ctx context.Context,
	t *testing.T,
	rpcClient *xrpl.RPCClient,
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
	autoFillTx(ctx, t, rpcClient, &tx, from)

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
	rpcClient *xrpl.RPCClient,
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
	autoFillTx(ctx, t, rpcClient, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	if ticketSeq != nil {
		tx.Sequence = 0
		tx.TicketSequence = ticketSeq
	}

	return tx
}
