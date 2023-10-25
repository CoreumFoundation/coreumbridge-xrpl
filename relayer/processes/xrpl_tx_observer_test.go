package processes_test

import (
	"context"
	"github.com/pkg/errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
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
		name                  string
		txScannerBuilder      func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner
		contractClientBuilder func(ctrl *gomock.Controller) processes.ContractClient
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
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().SendXRPLToCoreumTransferEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLToCoreumTransferEvidence{
						TxHash:    rippledata.Hash256{}.String(),
						Issuer:    xrplCurrencyAmount.Issuer.String(),
						Currency:  xrplCurrencyAmount.Currency.String(),
						Amount:    coreumAmount,
						Recipient: coreumRecipientAddress,
					},
				).Return(nil, nil)

				return contractClientMock
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
		{
			name: "outgoing_ticket_create_tx_with_sequence_number",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.TicketCreate{
									TxBase: rippledata.TxBase{
										Account:         bridgeAccount,
										Sequence:        5,
										TransactionType: rippledata.TICKET_CREATE,
									},
								},
								MetaData: createAllocatedTicketsMetaData([]uint32{3, 5, 7}),
							}
							cancel()
						}()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:         rippledata.Hash256{}.String(),
							SequenceNumber: lo.ToPtr(uint32(5)),
							Confirmed:      true,
						},
						Tickets: []uint32{3, 5, 7},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_ticket_create_tx_with_ticket_seq",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.TicketCreate{
									TxBase: rippledata.TxBase{
										Account:         bridgeAccount,
										TransactionType: rippledata.TICKET_CREATE,
									},
									TicketSequence: lo.ToPtr(uint32(11)),
								},
								MetaData: createAllocatedTicketsMetaData([]uint32{3, 5, 7}),
							}
							cancel()
						}()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:       rippledata.Hash256{}.String(),
							TicketNumber: lo.ToPtr(uint32(11)),
							Confirmed:    true,
						},
						Tickets: []uint32{3, 5, 7},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_ticket_create_tx_with_failure",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.TicketCreate{
									TxBase: rippledata.TxBase{
										Account:         bridgeAccount,
										Sequence:        5,
										TransactionType: rippledata.TICKET_CREATE,
									},
								},
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
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:         rippledata.Hash256{}.String(),
							SequenceNumber: lo.ToPtr(uint32(5)),
							Confirmed:      false,
						},
						Tickets: []uint32{},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_not_expected_tx",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						go func() {
							ch <- rippledata.TransactionWithMetaData{
								Transaction: &rippledata.TrustSet{
									TxBase: rippledata.TxBase{
										Account:         bridgeAccount,
										TransactionType: rippledata.NFTOKEN_CREATE_OFFER,
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
			var contractClient processes.ContractClient
			if tt.contractClientBuilder != nil {
				contractClient = tt.contractClientBuilder(ctrl)
			}
			o := processes.NewXRPLTxObserver(
				processes.XRPLTxObserverConfig{
					BridgeAccount:  bridgeAccount,
					RelayerAddress: relayerAddress,
				},
				logMock,
				tt.txScannerBuilder(ctrl, cancel),
				contractClient,
			)
			require.True(t, errors.Is(o.Start(ctx), context.Canceled), err)
		})
	}
}

func createAllocatedTicketsMetaData(ticketNumbers []uint32) rippledata.MetaData {
	nodeEffects := make(rippledata.NodeEffects, 0)
	for _, ticket := range ticketNumbers {
		ticketNodeField := &rippledata.Ticket{
			TicketSequence: lo.ToPtr(ticket),
		}
		ticketNodeField.LedgerEntryType = rippledata.TICKET
		nodeEffects = append(nodeEffects, rippledata.NodeEffect{
			CreatedNode: &rippledata.AffectedNode{
				NewFields: ticketNodeField,
			},
		})
	}

	return rippledata.MetaData{
		AffectedNodes: nodeEffects,
	}
}
