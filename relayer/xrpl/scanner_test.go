package xrpl_test

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/testutils"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestAccountScanner_ScanTxs(t *testing.T) {
	t.Parallel()

	// set the time to prevent infinite test
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	account := testutils.GenXRPLAccount()
	notEmptyMarker := map[string]any{"key": "val"}

	tests := []struct {
		name          string
		cfg           xrpl.AccountScannerConfig
		rpcTxProvider func(ctrl *gomock.Controller) xrpl.RPCTxProvider
		wantTxHashes  []string
		wantErr       bool
	}{
		{
			name: "full_scan_positive_with_retry_two_pages",
			cfg: xrpl.AccountScannerConfig{
				Account:         account,
				FullScanEnabled: true,
				RetryDelay:      time.Millisecond,
			},
			rpcTxProvider: func(ctrl *gomock.Controller) xrpl.RPCTxProvider {
				mockedProvider := NewMockRPCTxProvider(ctrl)
				callNumber := 0
				mockedProvider.EXPECT().AccountTx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, account rippledata.Account, minLedger, maxLedger int64, marker map[string]any) (xrpl.AccountTxResult, error) {
						callNumber++
						switch callNumber {
						case 1:
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"1": 1,
									"2": 2,
									"3": 3,
								}),
								Marker: notEmptyMarker,
							}, nil
						// emulate error
						case 2:
							return xrpl.AccountTxResult{}, errors.New("error")
						case 3:
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"4": 3,
									"5": 4,
								}),
							}, nil
						default:
							panic("unexpected call")
						}
					}).AnyTimes()
				return mockedProvider
			},
			wantTxHashes: []string{
				"1", "2", "3", "4", "5",
			},
			wantErr: false,
		},
		{
			name: "recent_scan_positive_with_retry_four_pages",
			cfg: xrpl.AccountScannerConfig{
				Account:           account,
				RecentScanEnabled: true,
				RecentScanWindow:  10,
				RepeatRecentScan:  true,
				RetryDelay:        time.Millisecond,
			},
			rpcTxProvider: func(ctrl *gomock.Controller) xrpl.RPCTxProvider {
				mockedProvider := NewMockRPCTxProvider(ctrl)

				mockedProvider.EXPECT().LedgerCurrent(ctx).Return(xrpl.LedgerCurrentResult{
					LedgerCurrentIndex: 100,
				}, nil)

				callNumber := 0
				mockedProvider.EXPECT().AccountTx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, account rippledata.Account, minLedger, maxLedger int64, marker map[string]any) (xrpl.AccountTxResult, error) {
						callNumber++
						switch callNumber {
						case 1:
							require.Equal(t, int64(100-10), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"1": 90,
									"2": 91,
									"3": 91,
									"4": 92,
								}),
								Marker: notEmptyMarker,
							}, nil
						case 2:
							require.Equal(t, int64(100-10), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"5": 92,
								}),
								// finish
								Marker: nil,
							}, nil
						case 3:
							require.Equal(t, int64(93), minLedger)
							return xrpl.AccountTxResult{}, errors.New("error")
						case 4:
							require.Equal(t, int64(93), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"6": 93,
								}),
								// finish
								Marker: nil,
							}, nil
						case 5:
							require.Equal(t, int64(94), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions(map[string]uint32{
									"7": 94,
								}),
							}, nil
						default:
							panic("unexpected call")
						}
					}).AnyTimes()
				return mockedProvider
			},
			wantTxHashes: []string{
				"1", "2", "3", "4", "5", "6", "7",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			zapDevLogger, err := zap.NewDevelopment()
			require.NoError(t, err)
			rpcTxProvider := tt.rpcTxProvider(ctrl)

			s := xrpl.NewAccountScanner(tt.cfg, logger.NewZapLoggerFromLogger(zapDevLogger), rpcTxProvider)
			txsCh := make(chan rippledata.TransactionWithMetaData)
			if err := s.ScanTxs(ctx, txsCh); (err != nil) != tt.wantErr {
				t.Errorf("ScanTxs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(tt.wantTxHashes) == 0 {
				return
			}
			// validate that we have received expected hashes
			gotTxHashes := readTxHashesFromChannels(ctx, t, txsCh, len(tt.wantTxHashes))
			require.Equal(t, lo.SliceToMap(tt.wantTxHashes, func(hash string) (string, struct{}) {
				return hash, struct{}{}
			}), gotTxHashes)
		})
	}
}

func readTxHashesFromChannels(ctx context.Context, t *testing.T, txsCh chan rippledata.TransactionWithMetaData, count int) map[string]struct{} {
	gotTxHashes := make(map[string]struct{})
	for {
		select {
		case <-ctx.Done():
			t.Fail()
		case tx := <-txsCh:
			decoded, err := hex.DecodeString(strings.TrimRight(tx.GetHash().String(), "0"))
			require.NoError(t, err)
			gotTxHashes[string(decoded)] = struct{}{}
			if len(gotTxHashes) == count {
				return gotTxHashes
			}
		}
	}
}

func buildEmptyTransactions(txsData map[string]uint32) []*rippledata.TransactionWithMetaData {
	txs := make([]*rippledata.TransactionWithMetaData, 0, len(txsData))
	for hash, ledgerSequence := range txsData {
		var txHash rippledata.Hash256
		copy(txHash[:], hash)
		txs = append(txs, &rippledata.TransactionWithMetaData{
			LedgerSequence: ledgerSequence,
			Transaction: &rippledata.Payment{
				TxBase: rippledata.TxBase{
					Hash: txHash,
				},
			},
		})
	}
	return txs
}
