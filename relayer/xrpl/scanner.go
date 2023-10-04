package xrpl

import (
	"context"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

//go:generate mockgen -destination=scanner_mocks_test.go -package=xrpl_test . RPCTxProvider

// RPCTxProvider is RPC transactions provider.
type RPCTxProvider interface {
	LedgerCurrent(ctx context.Context) (LedgerCurrentResult, error)
	AccountTx(ctx context.Context, account rippledata.Account, minLedger, maxLedger int64, marker map[string]any) (AccountTxResult, error)
}

// AccountScannerConfig is the AccountScanner config.
type AccountScannerConfig struct {
	Account rippledata.Account

	RecentScanEnabled bool
	RecentScanWindow  int64
	RepeatRecentScan  bool

	FullScanEnabled bool
	RepeatFullScan  bool

	RetryDelay time.Duration
}

// DefaultAccountScannerConfig returns the default AccountScannerConfig.
func DefaultAccountScannerConfig(account rippledata.Account) AccountScannerConfig {
	return AccountScannerConfig{
		Account: account,

		RecentScanEnabled: true,
		RecentScanWindow:  10_000,
		RepeatRecentScan:  true,

		FullScanEnabled: true,
		RepeatFullScan:  true,
		RetryDelay:      10 * time.Second,
	}
}

// AccountScanner is XRPL transactions scanner.
type AccountScanner struct {
	cfg           AccountScannerConfig
	log           logger.Logger
	rpcTxProvider RPCTxProvider
}

// NewAccountScanner returns a nw instance of the AccountScanner.
func NewAccountScanner(cfg AccountScannerConfig, log logger.Logger, rpcTxProvider RPCTxProvider) *AccountScanner {
	return &AccountScanner{
		cfg:           cfg,
		log:           log,
		rpcTxProvider: rpcTxProvider,
	}
}

// ScanTxs subscribes on rpc account transactions and continuously scans the recent and historical transactions.
func (s *AccountScanner) ScanTxs(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
	s.log.Info(ctx, "Subscribing xrpl scanner", logger.AnyFiled("config", s.cfg))
	if s.cfg.RecentScanEnabled {
		currentLedgerRes, err := s.rpcTxProvider.LedgerCurrent(ctx)
		if err != nil {
			return err
		}
		currentLedger := currentLedgerRes.LedgerCurrentIndex
		if currentLedger <= s.cfg.RecentScanWindow {
			return errors.Errorf("current ledger must be greater than the recent scan window, "+
				"currentLedger:%d, recentScanWindow:%d", currentLedger, s.cfg.RecentScanWindow)
		}
		go s.scanRecentHistory(ctx, currentLedger, ch)
	}

	if s.cfg.FullScanEnabled {
		go s.scanFullHistory(ctx, ch)
	}

	if !s.cfg.RecentScanEnabled && !s.cfg.FullScanEnabled {
		return errors.Errorf("both recent and full scans are disabled")
	}

	return nil
}

func (s *AccountScanner) scanRecentHistory(ctx context.Context, currentLedger int64, ch chan<- rippledata.TransactionWithMetaData) {
	minLedger := currentLedger - s.cfg.RecentScanWindow
	s.doWithRetry(ctx, s.cfg.RepeatRecentScan, func() {
		s.log.Info(ctx, "Scanning recent history", logger.Int64Filed("minLedger", minLedger))
		lastLedger := s.scanTransactions(ctx, minLedger, ch)
		if lastLedger != 0 {
			minLedger = lastLedger + 1
		}
		s.log.Info(ctx, "Scanning of the recent history is done", logger.Int64Filed("lastLedger", lastLedger))
	})
}

func (s *AccountScanner) scanFullHistory(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) {
	s.doWithRetry(ctx, s.cfg.RepeatFullScan, func() {
		s.log.Info(ctx, "Scanning full history")
		lastLedger := s.scanTransactions(ctx, -1, ch)
		s.log.Info(ctx, "Scanning of full history is done", logger.Int64Filed("lastLedger", lastLedger))
	})
}

func (s *AccountScanner) scanTransactions(ctx context.Context, minLedger int64, ch chan<- rippledata.TransactionWithMetaData) int64 {
	if minLedger <= 0 {
		minLedger = -1
	}
	var (
		marker              map[string]any
		lastLedger          int64
		prevProcessedLedger int64
	)
	for {
		var accountTxResult AccountTxResult
		err := retry.Do(ctx, s.cfg.RetryDelay, func() error {
			var err error
			accountTxResult, err = s.rpcTxProvider.AccountTx(ctx, s.cfg.Account, minLedger, -1, marker)
			if err != nil {
				return retry.Retryable(
					errors.Wrapf(err, "failed to get account transactions, account:%s, minLedger:%d, marker:%+v",
						s.cfg.Account.String(), minLedger, marker),
				)
			}
			return nil
		})
		if err != nil {
			if isCtxError(err) {
				return lastLedger
			}
			// this panic is unexpected
			panic(errors.Wrapf(err, "unexpected error received for the get account transactions with retry, err:%s", err.Error()))
		}

		for _, tx := range accountTxResult.Transactions {
			// init prev processed ledger wasn't initialized we expect that we processed the prev ledger
			if prevProcessedLedger == 0 {
				prevProcessedLedger = int64(tx.LedgerSequence)
			}
			if prevProcessedLedger < int64(tx.LedgerSequence) {
				lastLedger = prevProcessedLedger
				prevProcessedLedger = int64(tx.LedgerSequence)
			}
			ch <- *tx
		}
		if len(accountTxResult.Marker) == 0 {
			lastLedger = prevProcessedLedger
			break
		}
		marker = accountTxResult.Marker
	}

	return lastLedger
}

func (s *AccountScanner) doWithRetry(ctx context.Context, shouldRetry bool, f func()) {
	err := retry.Do(ctx, s.cfg.RetryDelay, func() error {
		f()
		if shouldRetry {
			s.log.Info(ctx, "Waiting before the next execution.", logger.StringFiled("retryDelay", s.cfg.RetryDelay.String()))
			return retry.Retryable(errors.New("repeat scan"))
		}
		s.log.Info(ctx, "Execution is fully stopped.")
		return nil
	})
	if err == nil || isCtxError(err) {
		return
	}
	// this panic is unexpected
	panic(errors.Wrap(err, "unexpected error in do with resubscribe"))
}

func isCtxError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
