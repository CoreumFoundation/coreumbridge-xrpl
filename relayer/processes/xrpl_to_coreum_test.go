package processes_test

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestXRPLToCoreumProcess_Start(t *testing.T) {
	t.Parallel()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account()
	recipientXRPLAddress := xrpl.GenPrivKeyTxSigner().Account()
	issuerAccount := xrpl.GenPrivKeyTxSigner().Account()

	// tecPATH_PARTIAL
	failTxResult := rippledata.TransactionResult(101)
	// tefBAD_AUTH
	notTxResult := rippledata.TransactionResult(-199)

	relayerAddress := coreum.GenAccount()
	coreumRecipientAddress := coreum.GenAccount()
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipientAddress)
	require.NoError(t, err)

	xrplCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)
	txValue, err := rippledata.NewValue("999", false)
	require.NoError(t, err)
	xrplOriginatedTokenXRPLAmount := rippledata.Amount{
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

	xrplOriginatedTokenPaymentWithMetadataTx := rippledata.TransactionWithMetaData{
		Transaction: &rippledata.Payment{
			Destination: bridgeXRPLAddress,
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
			DeliveredAmount: &xrplOriginatedTokenXRPLAmount,
		},
	}

	coreumOriginatedTokenXRPLAmount := rippledata.Amount{
		Value:    txValue,
		Currency: xrplCurrency,
		Issuer:   bridgeXRPLAddress,
	}
	coreumOriginatedTokenPaymentWithMetadataTx := rippledata.TransactionWithMetaData{
		Transaction: &rippledata.Payment{
			Destination: bridgeXRPLAddress,
			Amount:      coreumOriginatedTokenXRPLAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
				Memos: rippledata.Memos{
					memo,
				},
			},
		},
		MetaData: rippledata.MetaData{
			DeliveredAmount: &coreumOriginatedTokenXRPLAmount,
		},
	}

	tooHighValue, err := rippledata.NewValue("1e85", false)
	require.NoError(t, err)
	tooHighXRPLAmount := rippledata.Amount{
		Value:    tooHighValue,
		Currency: xrplCurrency,
		Issuer:   bridgeXRPLAddress,
	}
	coreumOriginatedTokenPaymentWithTooHighAmountAndMetadataTx := rippledata.TransactionWithMetaData{
		Transaction: &rippledata.Payment{
			Destination: bridgeXRPLAddress,
			Amount:      tooHighXRPLAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
				Memos: rippledata.Memos{
					memo,
				},
			},
		},
		MetaData: rippledata.MetaData{
			DeliveredAmount: &tooHighXRPLAmount,
		},
	}

	tests := []struct {
		name                  string
		errorsCount           int
		unexpectedTxCount     int
		txScannerBuilder      func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner
		contractClientBuilder func(ctrl *gomock.Controller) processes.ContractClient
	}{
		{
			name: "incoming_xrpl_originated_token_valid_payment",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- xrplOriginatedTokenPaymentWithMetadataTx
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLToCoreumTransferEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLToCoreumTransferEvidence{
						TxHash:    rippledata.Hash256{}.String(),
						Issuer:    xrplOriginatedTokenXRPLAmount.Issuer.String(),
						Currency:  xrpl.ConvertCurrencyToString(xrplOriginatedTokenXRPLAmount.Currency),
						Amount:    sdkmath.NewIntWithDecimal(999, xrpl.XRPLIssuedTokenDecimals),
						Recipient: coreumRecipientAddress,
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "incoming_coreum_originated_token_valid_payment",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- coreumOriginatedTokenPaymentWithMetadataTx
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)

				stringCurrency := xrpl.ConvertCurrencyToString(xrplOriginatedTokenXRPLAmount.Currency)
				contractClientMock.EXPECT().SendXRPLToCoreumTransferEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLToCoreumTransferEvidence{
						TxHash:    rippledata.Hash256{}.String(),
						Issuer:    bridgeXRPLAddress.String(),
						Currency:  stringCurrency,
						Amount:    sdkmath.NewIntWithDecimal(999, xrpl.XRPLIssuedTokenDecimals),
						Recipient: coreumRecipientAddress,
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "incoming_not_success_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.Payment{},
							MetaData: rippledata.MetaData{
								TransactionResult: failTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "incoming_not_confirmed_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.Payment{},
							MetaData: rippledata.MetaData{
								TransactionResult: notTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "incoming_not_payment_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TrustSet{
								TxBase: rippledata.TxBase{
									TransactionType: rippledata.TRUST_SET,
									Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
								},
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "incoming_coreum_originated_token_valid_payment_with_too_high_amount",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- coreumOriginatedTokenPaymentWithTooHighAmountAndMetadataTx
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "outgoing_ticket_create_tx_with_account_sequence",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TicketCreate{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									Sequence:        5,
									TransactionType: rippledata.TICKET_CREATE,
								},
							},
							MetaData: createAllocatedTicketsMetaData([]uint32{3, 5, 7}),
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							AccountSequence:   lo.ToPtr(uint32(5)),
							TransactionResult: coreum.TransactionResultAccepted,
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
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TicketCreate{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.TICKET_CREATE,
								},
								TicketSequence: lo.ToPtr(uint32(11)),
							},
							MetaData: createAllocatedTicketsMetaData([]uint32{3, 5, 7}),
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultAccepted,
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
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TicketCreate{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									Sequence:        5,
									TransactionType: rippledata.TICKET_CREATE,
								},
							},
							MetaData: rippledata.MetaData{
								TransactionResult: failTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTicketsAllocationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							AccountSequence:   lo.ToPtr(uint32(5)),
							TransactionResult: coreum.TransactionResultRejected,
						},
						Tickets: nil,
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_trust_set_tx",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TrustSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.TRUST_SET,
									Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
								},
								LimitAmount:    xrplOriginatedTokenXRPLAmount,
								TicketSequence: lo.ToPtr(uint32(11)),
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLTrustSetTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTrustSetEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultAccepted,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_trust_set_tx_with_failure",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TrustSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.TRUST_SET,
									Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
								},
								LimitAmount:    xrplOriginatedTokenXRPLAmount,
								TicketSequence: lo.ToPtr(uint32(11)),
							},
							MetaData: rippledata.MetaData{
								TransactionResult: failTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendXRPLTrustSetTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultTrustSetEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultRejected,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_payment_tx",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.Payment{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.PAYMENT,
								},
								Destination:    recipientXRPLAddress,
								Amount:         xrplOriginatedTokenXRPLAmount,
								TicketSequence: lo.ToPtr(uint32(11)),
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendCoreumToXRPLTransferTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultAccepted,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_payment_tx_with_failure",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.Payment{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.PAYMENT,
								},
								Destination:    recipientXRPLAddress,
								Amount:         xrplOriginatedTokenXRPLAmount,
								TicketSequence: lo.ToPtr(uint32(11)),
							},
							MetaData: rippledata.MetaData{
								TransactionResult: failTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendCoreumToXRPLTransferTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultRejected,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_signer_list_set_tx_with_ticket_seq",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.SignerListSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.SIGNER_LIST_SET,
									Signers:         []rippledata.Signer{{}},
								},
								TicketSequence: lo.ToPtr(uint32(11)),
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendKeysRotationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultKeysRotationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultAccepted,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_signer_list_set_tx_with_account_seq",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.SignerListSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.SIGNER_LIST_SET,
									Sequence:        uint32(9),
								},
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
		},
		{
			name: "outgoing_signer_list_set_tx_with_failure",
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.SignerListSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.SIGNER_LIST_SET,
									Signers:         []rippledata.Signer{{}},
								},
								TicketSequence: lo.ToPtr(uint32(11)),
							},
							MetaData: rippledata.MetaData{
								TransactionResult: failTxResult,
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				contractClientMock.EXPECT().SendKeysRotationTransactionResultEvidence(
					gomock.Any(),
					relayerAddress,
					coreum.XRPLTransactionResultKeysRotationEvidence{
						XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
							TxHash:            rippledata.Hash256{}.String(),
							TicketSequence:    lo.ToPtr(uint32(11)),
							TransactionResult: coreum.TransactionResultRejected,
						},
					},
				).Return(nil, nil)

				return contractClientMock
			},
		},
		{
			name: "outgoing_not_expected_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().IsInitialized().Return(true)
				return contractClientMock
			},
			txScannerBuilder: func(ctrl *gomock.Controller, cancel func()) processes.XRPLAccountTxScanner {
				xrplAccountTxScannerMock := NewMockXRPLAccountTxScanner(ctrl)
				xrplAccountTxScannerMock.EXPECT().ScanTxs(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
						ch <- rippledata.TransactionWithMetaData{
							Transaction: &rippledata.TrustSet{
								TxBase: rippledata.TxBase{
									Account:         bridgeXRPLAddress,
									TransactionType: rippledata.NFTOKEN_CREATE_OFFER,
								},
							},
						}
						cancel()
						return nil
					})

				return xrplAccountTxScannerMock
			},
			unexpectedTxCount: 1,
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
			logMock.EXPECT().Error(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			var contractClient processes.ContractClient
			if tt.contractClientBuilder != nil {
				contractClient = tt.contractClientBuilder(ctrl)
			}
			metricRegistryMock := NewMockMetricRegistry(ctrl)
			if tt.unexpectedTxCount > 0 {
				metricRegistryMock.EXPECT().SetMaliciousBehaviourKey(gomock.Any()).Times(tt.unexpectedTxCount)
			}
			metricRegistryMock.EXPECT().DeleteMaliciousBehaviourKey(gomock.Any()).AnyTimes()
			o, err := processes.NewXRPLToCoreumProcess(
				processes.XRPLToCoreumProcessConfig{
					BridgeXRPLAddress:    bridgeXRPLAddress,
					RelayerCoreumAddress: relayerAddress,
				},
				logMock,
				tt.txScannerBuilder(ctrl, cancel),
				contractClient,
				metricRegistryMock,
			)
			require.NoError(t, err)
			require.ErrorIs(t, o.Start(ctx), context.Canceled)
		})
	}
}

func createAllocatedTicketsMetaData(ticketSequences []uint32) rippledata.MetaData {
	nodeEffects := make(rippledata.NodeEffects, 0)
	for _, ticket := range ticketSequences {
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
