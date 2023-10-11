package processes_test

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/testutils"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestXRPLTxObserver_Start(t *testing.T) {
	t.Parallel()

	bridgeAccount := testutils.GenXRPLAccount()
	issuerAccount := testutils.GenXRPLAccount()

	relayerAddress := testutils.GenCoreumAccount()
	coreumRecipientAddress := testutils.GenCoreumAccount()
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipientAddress)
	require.NoError(t, err)

	xrplCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)
	txValue, err := rippledata.NewValue("999", false)
	require.NoError(t, err)
	xrplCurrencyAmount := rippledata.Amount{
		Value:    txValue,
		Currency: xrplCurrency,
		Issuer:   issuerAccount,
	}

	increasedTxValue, err := rippledata.NewValue("999000", false)
	require.NoError(t, err)
	xrplCurrencyIncreasedAmount := rippledata.Amount{
		Value:    increasedTxValue,
		Currency: xrplCurrency,
		Issuer:   issuerAccount,
	}

	coreumAmount := sdkmath.NewIntWithDecimal(999, 15)

	paymentWithMetadataTx := rippledata.TransactionWithMetaData{
		Transaction: &rippledata.Payment{
			Destination: bridgeAccount,
			// amount is increased to check that we use the delivered amount
			Amount: xrplCurrencyIncreasedAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
				Memos: rippledata.Memos{
					memo,
				},
			},
		},
		MetaData: rippledata.MetaData{
			DeliveredAmount: &xrplCurrencyAmount,
		},
	}

	tests := []struct {
		name                     string
		txScannerBuilder         func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner
		evidencesConsumerBuilder func(ctrl *gomock.Controller) processes.EvidencesConsumer
	}{
		{
			name: "incoming_valid_payment",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- paymentWithMetadataTx
							cancel()
						}()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			evidencesConsumerBuilder: func(ctrl *gomock.Controller) processes.EvidencesConsumer {
				evidencesConsumer := NewMockEvidencesConsumer(ctrl)
				evidencesConsumer.EXPECT().SendXRPLToCoreumTransferEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLToCoreumEvidence{
						TxHash:    rippledata.Hash256{}.String(),
						Issuer:    xrplCurrencyAmount.Issuer.String(),
						Currency:  xrplCurrencyAmount.Currency.String(),
						Amount:    coreumAmount,
						Recipient: coreumRecipientAddress,
					},
				).Return(nil, nil)

				return evidencesConsumer
			},
		},
		{
			name: "incoming_not_success_tx",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.Payment{},
								MetaData: rippledata.MetaData{
									// if code not 0 - not success
									TransactionResult: rippledata.TransactionResult(111),
								},
							}
							cancel()
						}()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "incoming_not_payment_tx",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.TrustSet{
									TxBase: rippledata.TxBase{
										TransactionType: rippledata.TRUST_SET,
									},
								},
							}
							cancel()
						}()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			ctrl := gomock.NewController(t)
			logMock := logger.NewAnyLogMock(ctrl)
			var evidencesConsumer processes.EvidencesConsumer
			if tt.evidencesConsumerBuilder != nil {
				evidencesConsumer = tt.evidencesConsumerBuilder(ctrl)
			}
			o := processes.NewXRPLTxObserver(
				processes.XRPLTxObserverConfig{
					BridgeAccount: bridgeAccount,
				},
				logMock,
				relayerAddress,
				tt.txScannerBuilder(ctrl, cancel),
				evidencesConsumer,
			)
			require.ErrorIs(t, context.Canceled, o.Start(ctx))
		})
	}
}
