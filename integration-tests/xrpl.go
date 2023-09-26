package integrationtests

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	ripplecrypto "github.com/rubblelabs/ripple/crypto"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client/xrpl"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

const (
	// XRPCurrencyCode is XRP toke currency code on XRPL chain.
	XRPCurrencyCode = "XRP"

	xrplTxFee                   = "100"
	xrplRserveToActivateAccount = float64(10)
	ecdsaKeyType                = rippledata.ECDSA
)

// ********** Wallet **********

// XRPLWallet is XRPL wallet.
type XRPLWallet struct {
	Key      ripplecrypto.Key
	Sequence *uint32
	Account  rippledata.Account
}

// NewXRPLWalletFromSeedPhrase returns the XRPLWallet generated for the provided seed.
func NewXRPLWalletFromSeedPhrase(seedPhrase string) (XRPLWallet, error) {
	seed, err := rippledata.NewSeedFromAddress(seedPhrase)
	if err != nil {
		return XRPLWallet{}, errors.Wrapf(err, "failed to create wallet from seed")
	}

	key := seed.Key(ecdsaKeyType)
	seq := lo.ToPtr(uint32(0))
	account := seed.AccountId(ecdsaKeyType, seq)

	return XRPLWallet{
		Key:      key,
		Sequence: seq,
		Account:  account,
	}, nil
}

// MultiSign multi-signs the transaction and returns the signer.
func (w XRPLWallet) MultiSign(tx rippledata.MultiSignable) (rippledata.Signer, error) {
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

// ********** XRPLChain **********

// XRPLChainConfig is a config required for the XRPL chain to be created.
type XRPLChainConfig struct {
	RPCAddress  string
	FundingSeed string
}

// XRPLChain is XRPL chain for the testing.
type XRPLChain struct {
	cfg           XRPLChainConfig
	fundingWallet XRPLWallet
	rpcClient     *xrpl.RPCClient
	fundMu        *sync.Mutex
}

// NewXRPLChain returns the new instance of the XRPL chain.
func NewXRPLChain(cfg XRPLChainConfig, log logger.Logger) (XRPLChain, error) {
	fundingWallet, err := NewXRPLWalletFromSeedPhrase(cfg.FundingSeed)
	if err != nil {
		return XRPLChain{}, err
	}

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(cfg.RPCAddress),
		log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	return XRPLChain{
		cfg:           cfg,
		fundingWallet: fundingWallet,
		rpcClient:     rpcClient,
		fundMu:        &sync.Mutex{},
	}, nil
}

// Config returns the chain config.
func (c XRPLChain) Config() XRPLChainConfig {
	return c.cfg
}

// RPCClient returns the XRPL RPC client.
func (c XRPLChain) RPCClient() *xrpl.RPCClient {
	return c.rpcClient
}

// GenWallet generates the active wallet with the initial provided amount.
func (c XRPLChain) GenWallet(ctx context.Context, t *testing.T, amount float64) XRPLWallet {
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

	wallet, err := NewXRPLWalletFromSeedPhrase(seed.String())
	require.NoError(t, err)

	c.FundAccount(ctx, t, wallet.Account, amount+xrplRserveToActivateAccount)

	return wallet
}

// FundAccount funds the provided account with the provided amount.
func (c XRPLChain) FundAccount(ctx context.Context, t *testing.T, acc rippledata.Account, amount float64) {
	t.Helper()

	c.fundMu.Lock()
	defer c.fundMu.Unlock()

	xrpAmount, err := rippledata.NewAmount(fmt.Sprintf("%f%s", amount, XRPCurrencyCode))
	require.NoError(t, err)
	fundXrpTx := rippledata.Payment{
		Destination: acc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	t.Logf("Funding account, account address: %s, amount: %f", acc, amount)
	require.NoError(t, c.AutoFillSignAndSubmitTx(ctx, t, &fundXrpTx, c.fundingWallet))
	t.Logf("The account %s is funded", acc)
}

// AutoFillSignAndSubmitTx autofills the transaction and submits it.
func (c XRPLChain) AutoFillSignAndSubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, wallet XRPLWallet) error {
	t.Helper()

	c.AutoFillTx(ctx, t, tx, wallet.Account)
	return c.SignAndSubmitTx(ctx, t, tx, wallet)
}

// SignAndSubmitTx signs the transaction from the wallet and submits it.
func (c XRPLChain) SignAndSubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, wallet XRPLWallet) error {
	t.Helper()

	require.NoError(t, rippledata.Sign(tx, wallet.Key, wallet.Sequence))
	return c.SubmitTx(ctx, t, tx)
}

// AutoFillTx add seq number and fee for the transaction.
func (c XRPLChain) AutoFillTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, sender rippledata.Account) {
	t.Helper()

	accInfo, err := c.rpcClient.AccountInfo(ctx, sender)
	require.NoError(t, err)
	// update base settings
	base := tx.GetBase()
	fee, err := rippledata.NewValue(xrplTxFee, true)
	require.NoError(t, err)
	base.Fee = *fee
	base.Account = sender
	base.Sequence = *accInfo.AccountData.Sequence
}

// SubmitTx submits tx a waits for its result.
func (c XRPLChain) SubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction) error {
	t.Helper()

	t.Logf("Submitting transaction, hash:%s", tx.GetHash())
	// submit the transaction
	res, err := c.rpcClient.Submit(ctx, tx)
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
		txRes, err := c.rpcClient.Tx(reqCtx, *tx.GetHash())
		if err != nil {
			return retry.Retryable(err)
		}

		if !txRes.Validated {
			return retry.Retryable(errors.Errorf("transaction is not validated"))
		}
		return nil
	})
}

// GetAccountBalances returns account balances.
func (c XRPLChain) GetAccountBalances(ctx context.Context, t *testing.T, acc rippledata.Account) map[string]rippledata.Amount {
	t.Helper()

	amounts := make(map[string]rippledata.Amount, 0)

	accInfo, err := c.rpcClient.AccountInfo(ctx, acc)
	require.NoError(t, err)
	amounts[XRPCurrencyCode] = rippledata.Amount{
		Value: accInfo.AccountData.Balance,
	}
	// none xrp amounts
	accLines, err := c.rpcClient.AccountLines(ctx, acc, "closed", nil)
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

// AwaitLedger awaits for ledger index.
func (c XRPLChain) AwaitLedger(ctx context.Context, t *testing.T, ledgerIndex int64) {
	t.Helper()

	t.Logf("Waiting for the ledger:%d", ledgerIndex)
	retryCtx, retryCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer retryCtxCancel()
	require.NoError(t, retry.Do(retryCtx, 250*time.Millisecond, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(retryCtx, 3*time.Second)
		defer reqCtxCancel()
		res, err := c.rpcClient.LedgerCurrent(reqCtx)
		if err != nil {
			return retry.Retryable(err)
		}

		if res.LedgerCurrentIndex < ledgerIndex {
			return retry.Retryable(errors.Errorf("ledger has not passed, current:%d, expected:%d", res.LedgerCurrentIndex, ledgerIndex))
		}

		return nil
	}))
}

// ExtractTicketsFromMeta extracts tickets info from the tx metadata.
func ExtractTicketsFromMeta(txRes xrpl.TxResult) []*rippledata.Ticket {
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
