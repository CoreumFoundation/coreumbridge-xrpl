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

func TestXRPLTxSubmitter_Start(t *testing.T) {
	t.Parallel()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account()
	xrplTxSignerKeyName := "xrpl-tx-signer"

	xrplRelayer1Signer := xrpl.GenPrivKeyTxSigner()
	xrplRelayer2Signer := xrpl.GenPrivKeyTxSigner()
	xrplRelayer3Signer := xrpl.GenPrivKeyTxSigner()

	coreumRelayer1Address := coreum.GenAccount()
	coreumRelayer2Address := coreum.GenAccount()
	coreumRelayer3Address := coreum.GenAccount()

	xrplSigners := []rippledata.Account{
		xrplRelayer1Signer.Account(),
		xrplRelayer2Signer.Account(),
		xrplRelayer3Signer.Account(),
	}
	signerQuorum := uint32(2)

	signerEntries := make([]rippledata.SignerEntry, 0, len(xrplSigners))
	for _, signerAcc := range xrplSigners {
		signerAcc := signerAcc
		signerEntries = append(signerEntries, rippledata.SignerEntry{
			SignerEntry: rippledata.SignerEntryItem{
				Account:      &signerAcc,
				SignerWeight: lo.ToPtr(uint16(1)),
			},
		})
	}

	contractRelayers := []coreum.Relayer{
		{
			CoreumAddress: coreumRelayer1Address,
			XRPLAddress:   xrplRelayer1Signer.Account().String(),
			XRPLPubKey:    xrplRelayer1Signer.PubKey().String(),
		},
		{
			CoreumAddress: coreumRelayer2Address,
			XRPLAddress:   xrplRelayer2Signer.Account().String(),
			XRPLPubKey:    xrplRelayer2Signer.PubKey().String(),
		},
		{
			CoreumAddress: coreumRelayer3Address,
			XRPLAddress:   xrplRelayer3Signer.Account().String(),
			XRPLPubKey:    xrplRelayer3Signer.PubKey().String(),
		},
	}

	bridgeXRPLSignerAccountWithSigners := xrpl.AccountInfoResult{
		AccountData: xrpl.AccountDataWithSigners{
			AccountRoot: rippledata.AccountRoot{
				Sequence: lo.ToPtr(uint32(1)),
			},
			SignerList: []rippledata.SignerList{{
				SignerEntries: signerEntries,
				SignerQuorum:  &signerQuorum,
			}},
		},
	}

	// ********** Allocate ticket **********

	allocateTicketOperationWithoutSignatures := coreum.Operation{
		AccountSequence: 1,
		Signatures:      nil,
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: 3,
			},
		},
	}

	allocateTicketOperationWithUnexpectedSequencNumber := coreum.Operation{
		AccountSequence: 999,
		Signatures:      nil,
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: 3,
			},
		},
	}

	allocateTicketOperationWithSignatures := allocateTicketOperationWithoutSignatures
	allocateTicketOperationSigner1 := multiSignAllocateTicketsOperation(
		t,
		xrplRelayer1Signer,
		bridgeXRPLAddress,
		allocateTicketOperationWithSignatures,
	)
	allocateTicketOperationSigner2 := multiSignAllocateTicketsOperation(
		t,
		xrplRelayer2Signer,
		bridgeXRPLAddress,
		allocateTicketOperationWithSignatures,
	)
	allocateTicketOperationWithSignatures.Signatures = []coreum.Signature{
		{
			RelayerCoreumAddress: coreumRelayer1Address,
			Signature:            allocateTicketOperationSigner1.Signer.TxnSignature.String(),
		},
		{
			RelayerCoreumAddress: coreumRelayer2Address,
			Signature:            allocateTicketOperationSigner2.Signer.TxnSignature.String(),
		},
		{
			RelayerCoreumAddress: coreumRelayer3Address,
			// the signature is taken from the first signer, so it is invalid
			Signature: allocateTicketOperationSigner1.Signer.TxnSignature.String(),
		},
	}
	allocateTicketOperationWithSignaturesSigners := []rippledata.Signer{
		allocateTicketOperationSigner1,
		allocateTicketOperationSigner2,
	}

	// ********** TrustSet **********

	trustSetOperationWithoutSignatures := coreum.Operation{
		AccountSequence: 1,
		Signatures:      nil,
		OperationType: coreum.OperationType{
			TrustSet: &coreum.OperationTypeTrustSet{
				Issuer:              xrpl.GenPrivKeyTxSigner().Account().String(),
				Currency:            "TRC",
				TrustSetLimitAmount: sdkmath.NewInt(1000000000000),
			},
		},
	}

	trustSetOperationWithSignatures := trustSetOperationWithoutSignatures
	trustSetOperationSigner1 := multiSignTrustSetOperation(
		t,
		xrplRelayer1Signer,
		bridgeXRPLAddress,
		trustSetOperationWithSignatures,
	)
	trustSetOperationSigner2 := multiSignTrustSetOperation(
		t,
		xrplRelayer2Signer,
		bridgeXRPLAddress,
		trustSetOperationWithSignatures,
	)
	trustSetOperationWithSignatures.Signatures = []coreum.Signature{
		{
			RelayerCoreumAddress: coreumRelayer1Address,
			Signature:            trustSetOperationSigner1.Signer.TxnSignature.String(),
		},
		{
			RelayerCoreumAddress: coreumRelayer2Address,
			Signature:            trustSetOperationSigner2.Signer.TxnSignature.String(),
		},
		{
			RelayerCoreumAddress: coreumRelayer3Address,
			// the signature is taken from the first signer, so it is invalid
			Signature: trustSetOperationSigner1.Signer.TxnSignature.String(),
		},
	}
	trustSetOperationWithSignaturesSigners := []rippledata.Signer{
		trustSetOperationSigner1,
		trustSetOperationSigner2,
	}

	tests := []struct {
		name                  string
		contractClientBuilder func(ctrl *gomock.Controller) processes.ContractClient
		xrplRPCClientBuilder  func(ctrl *gomock.Controller) processes.XRPLRPCClient
		xrplTxSignerBuilder   func(ctrl *gomock.Controller) processes.XRPLTxSigner
	}{
		{
			name: "no_pending_operations",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{}, nil)
				return contractClientMock
			},
		},
		{
			name: "resister_signature_for_create_ticket_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{allocateTicketOperationWithoutSignatures}, nil)
				contractClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{
					Relayers: contractRelayers,
				}, nil)
				contractClientMock.EXPECT().SaveSignature(gomock.Any(), coreumRelayer1Address, allocateTicketOperationWithoutSignatures.AccountSequence, allocateTicketOperationSigner1.Signer.TxnSignature.String())
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				// 2 times one for the signatures and one more for the seq number
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), bridgeXRPLAddress).Return(bridgeXRPLSignerAccountWithSigners, nil).Times(2)
				return xrplRPCClientMock
			},
			xrplTxSignerBuilder: func(ctrl *gomock.Controller) processes.XRPLTxSigner {
				xrplTxSignerMock := NewMockXRPLTxSigner(ctrl)
				tx, err := processes.BuildTicketCreateTxForMultiSigning(bridgeXRPLAddress, allocateTicketOperationWithoutSignatures)
				require.NoError(t, err)
				xrplTxSignerMock.EXPECT().MultiSign(tx, xrplTxSignerKeyName).Return(allocateTicketOperationSigner1, nil)

				return xrplTxSignerMock
			},
		},
		{
			name: "submit_create_ticket_tx_with_filtered_signature",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{allocateTicketOperationWithSignatures}, nil)
				contractClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{
					Relayers: contractRelayers,
				}, nil)
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), bridgeXRPLAddress).Return(bridgeXRPLSignerAccountWithSigners, nil)
				expectedTx, err := processes.BuildTicketCreateTxForMultiSigning(bridgeXRPLAddress, allocateTicketOperationWithSignatures)
				require.NoError(t, err)
				require.NoError(t, rippledata.SetSigners(expectedTx, allocateTicketOperationWithSignaturesSigners...))
				xrplRPCClientMock.EXPECT().Submit(gomock.Any(), gomock.Any()).Do(func(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error) {
					_, expectedTxRaw, err := rippledata.Raw(expectedTx)
					require.NoError(t, err)
					_, txRaw, err := rippledata.Raw(tx)
					require.NoError(t, err)
					require.Equal(t, expectedTxRaw, txRaw)
					return xrpl.SubmitResult{}, nil
				})

				return xrplRPCClientMock
			},
		},
		{
			name: "resister_invalid_create_ticket_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{allocateTicketOperationWithUnexpectedSequencNumber}, nil)
				contractClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{
					Relayers: contractRelayers,
				}, nil)
				contractClientMock.EXPECT().SendXRPLTicketsAllocationTransactionResultEvidence(gomock.Any(), coreumRelayer1Address, coreum.XRPLTransactionResultTicketsAllocationEvidence{
					XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
						AccountSequence:   &allocateTicketOperationWithUnexpectedSequencNumber.AccountSequence,
						TransactionResult: coreum.TransactionResultInvalid,
					},
				})
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				// 2 times one for the signatures and one more for the seq number
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), bridgeXRPLAddress).Return(bridgeXRPLSignerAccountWithSigners, nil).Times(2)
				return xrplRPCClientMock
			},
		},
		{
			name: "resister_signature_for_trust_set_tx",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{trustSetOperationWithoutSignatures}, nil)
				contractClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{
					Relayers: contractRelayers,
				}, nil)
				contractClientMock.EXPECT().SaveSignature(gomock.Any(), coreumRelayer1Address, trustSetOperationWithoutSignatures.AccountSequence, trustSetOperationSigner1.Signer.TxnSignature.String())
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), bridgeXRPLAddress).Return(bridgeXRPLSignerAccountWithSigners, nil)
				return xrplRPCClientMock
			},
			xrplTxSignerBuilder: func(ctrl *gomock.Controller) processes.XRPLTxSigner {
				xrplTxSignerMock := NewMockXRPLTxSigner(ctrl)
				tx, err := processes.BuildTrustSetTxForMultiSigning(bridgeXRPLAddress, trustSetOperationWithoutSignatures)
				require.NoError(t, err)
				xrplTxSignerMock.EXPECT().MultiSign(tx, xrplTxSignerKeyName).Return(trustSetOperationSigner1, nil)

				return xrplTxSignerMock
			},
		},
		{
			name: "submit_trust_set_tx_with_filtered_signature",
			contractClientBuilder: func(ctrl *gomock.Controller) processes.ContractClient {
				contractClientMock := NewMockContractClient(ctrl)
				contractClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{trustSetOperationWithSignatures}, nil)
				contractClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{
					Relayers: contractRelayers,
				}, nil)
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), bridgeXRPLAddress).Return(bridgeXRPLSignerAccountWithSigners, nil)
				expectedTx, err := processes.BuildTrustSetTxForMultiSigning(bridgeXRPLAddress, trustSetOperationWithSignatures)
				require.NoError(t, err)
				require.NoError(t, rippledata.SetSigners(expectedTx, trustSetOperationWithSignaturesSigners...))
				xrplRPCClientMock.EXPECT().Submit(gomock.Any(), gomock.Any()).Do(func(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error) {
					_, expectedTxRaw, err := rippledata.Raw(expectedTx)
					require.NoError(t, err)
					_, txRaw, err := rippledata.Raw(tx)
					require.NoError(t, err)
					require.Equal(t, expectedTxRaw, txRaw)
					return xrpl.SubmitResult{}, nil
				})

				return xrplRPCClientMock
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

			var xrplRPCClient processes.XRPLRPCClient
			if tt.xrplRPCClientBuilder != nil {
				xrplRPCClient = tt.xrplRPCClientBuilder(ctrl)
			}

			var xrplTxSigner processes.XRPLTxSigner
			if tt.xrplTxSignerBuilder != nil {
				xrplTxSigner = tt.xrplTxSignerBuilder(ctrl)
			}

			o := processes.NewXRPLTxSubmitter(
				processes.XRPLTxSubmitterConfig{
					BridgeAccount:       bridgeXRPLAddress,
					RelayerAddress:      coreumRelayer1Address,
					XRPLTxSignerKeyName: xrplTxSignerKeyName,
				},
				logMock,
				contractClient,
				xrplRPCClient,
				xrplTxSigner,
			)
			require.NoError(t, o.Start(ctx))
		})
	}
}

func multiSignAllocateTicketsOperation(
	t *testing.T,
	relayerXRPLSigner *xrpl.PrivKeyTxSigner,
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) rippledata.Signer {
	tx, err := processes.BuildTicketCreateTxForMultiSigning(bridgeXRPLAddress, operation)
	require.NoError(t, err)
	signer, err := relayerXRPLSigner.MultiSign(tx)
	require.NoError(t, err)

	return signer
}

func multiSignTrustSetOperation(
	t *testing.T,
	relayerXRPLSigner *xrpl.PrivKeyTxSigner,
	bridgeXRPLAcc rippledata.Account,
	operation coreum.Operation,
) rippledata.Signer {
	tx, err := processes.BuildTrustSetTxForMultiSigning(bridgeXRPLAcc, operation)
	require.NoError(t, err)
	signer, err := relayerXRPLSigner.MultiSign(tx)
	require.NoError(t, err)

	return signer
}
