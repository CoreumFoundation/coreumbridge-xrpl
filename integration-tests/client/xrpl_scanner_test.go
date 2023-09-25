//go:build integrationtests
// +build integrationtests

package client_test

import (
	"context"
	"testing"
	"time"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/http"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client/xrpl"
)

func TestFullHistoryScanAccountTx(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	const txsCount = 20

	senderWallet := chains.XRPL.GenWallet(ctx, t, 100)
	t.Logf("Sender account: %s", senderWallet.Account)

	recipientWallet := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	// generate txs
	writtenTxHashes := sendMultipleTxs(ctx, t, txsCount, senderWallet, recipientWallet, chains.XRPL)

	rpcClientConfig := xrpl.DefaultRPCClientConfig(chains.XRPL.Config().RPCAddress)
	// update the page limit to low to emulate multiple pages
	rpcClientConfig.PageLimit = 2
	rpcClient := xrpl.NewRPCClient(
		rpcClientConfig,
		chains.Log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)
	// enable just historical scan
	scannerCfg := xrpl.AccountScannerConfig{
		Account:           senderWallet.Account,
		RecentScanEnabled: false,
		FullScanEnabled:   true,
		RetryDelay:        time.Second,
	}
	scanner := xrpl.NewAccountScanner(scannerCfg, chains.Log, rpcClient)
	// add timeout to finish the tests in case of error

	txsCh := make(chan rippledata.Transaction, txsCount)
	require.NoError(t, scanner.ScanTxs(ctx, txsCh))
	t.Logf("Waiting for %d transactions to be scanned by the historycal scanner", len(writtenTxHashes))
	validateTxsHashesInChannel(ctx, t, writtenTxHashes, txsCh)
}

func TestRecentHistoryScanAccountTx(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	const txsCount = 20

	senderWallet := chains.XRPL.GenWallet(ctx, t, 100)
	t.Logf("Sender account: %s", senderWallet.Account)

	recipientWallet := chains.XRPL.GenWallet(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	rpcClientConfig := xrpl.DefaultRPCClientConfig(chains.XRPL.Config().RPCAddress)
	// update the page limit to low to emulate multiple pages
	rpcClientConfig.PageLimit = 2
	rpcClient := xrpl.NewRPCClient(
		rpcClientConfig,
		chains.Log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	// update config to use recent scan only
	scannerCfg := xrpl.AccountScannerConfig{
		Account:           senderWallet.Account,
		RecentScanEnabled: true,
		RecentScanWindow:  5,
		RepeatRecentScan:  true,
		FullScanEnabled:   false,
		RetryDelay:        500 * time.Millisecond,
	}
	scanner := xrpl.NewAccountScanner(scannerCfg, chains.Log, rpcClient)

	// await for the state when the current ledger is valid to run the scanner
	currentLedger, err := chains.XRPL.RPCClient().LedgerCurrent(ctx)
	require.NoError(t, err)
	chains.XRPL.AwaitLedger(ctx, t,
		currentLedger.LedgerCurrentIndex+
			scannerCfg.RecentScanWindow)

	var writtenTxHashes map[string]struct{}
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		writtenTxHashes = sendMultipleTxs(ctx, t, 20, senderWallet, recipientWallet, chains.XRPL)
	}()

	txsCh := make(chan rippledata.Transaction, txsCount)
	require.NoError(t, scanner.ScanTxs(ctx, txsCh))

	t.Logf("Waiting for %d transactions to be scanned by the recent scanner", len(writtenTxHashes))
	receivedTxHashes := getTxHashesFromChannel(ctx, t, txsCh, txsCount)

	// wait for the writing to be done
	select {
	case <-ctx.Done():
		t.FailNow()
	case <-writeDone:
	}
	require.Equal(t, writtenTxHashes, receivedTxHashes)
}

func sendMultipleTxs(
	ctx context.Context,
	t *testing.T,
	count int,
	senderWallet, recipientWallet integrationtests.XRPLWallet,
	xrplChain integrationtests.XRPLChain,
) map[string]struct{} {
	writtenTxHashes := make(map[string]struct{})
	for i := 0; i < count; i++ {
		xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
		require.NoError(t, err)
		xrpPaymentTx := rippledata.Payment{
			Destination: recipientWallet.Account,
			Amount:      *xrpAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
			},
		}
		require.NoError(t, xrplChain.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderWallet))
		writtenTxHashes[xrpPaymentTx.Hash.String()] = struct{}{}
	}
	t.Logf("Successfully sent %d transactions", len(writtenTxHashes))
	return writtenTxHashes
}

func validateTxsHashesInChannel(ctx context.Context, t *testing.T, writtenTxHashes map[string]struct{}, txsCh chan rippledata.Transaction) {
	scanCtx, scanCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer scanCtxCancel()
	// copy the original map
	expectedHashes := make(map[string]struct{}, len(writtenTxHashes))
	for k, v := range writtenTxHashes {
		expectedHashes[k] = v
	}
	for {
		select {
		case <-scanCtx.Done():
			t.Fail()
		case tx := <-txsCh:
			// validate that we have all sent hashed and no duplicated
			hash := tx.GetHash().String()
			_, found := expectedHashes[hash]
			require.True(t, found)
			delete(expectedHashes, hash)
			if len(expectedHashes) == 0 {
				return
			}
		}
	}
}

func getTxHashesFromChannel(ctx context.Context, t *testing.T, txsCh chan rippledata.Transaction, count int) map[string]struct{} {
	scanCtx, scanCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer scanCtxCancel()
	txHashes := make(map[string]struct{}, count)
	for {
		select {
		case <-scanCtx.Done():
			t.Fail()
		case tx := <-txsCh:
			txHashes[tx.GetHash().String()] = struct{}{}
			if len(txHashes) == count {
				return txHashes
			}
		}
	}
}
