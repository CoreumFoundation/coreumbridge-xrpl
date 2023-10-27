package processes_test

import (
	"context"
	"testing"

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

func TestXRPLTxSubmitter_Start(t *testing.T) {
	t.Parallel()

	xrplBridgeAccount := testutils.GenXRPLAccount()
	xrplTxSignerKeyName := "xrpl-tx-signer"

	xrplRelayer1Account := testutils.GenXRPLAccount()
	xrplRelayer2Account := testutils.GenXRPLAccount()
	xrplRelayer3Account := testutils.GenXRPLAccount()

	xrplRelayer1PubKey := testutils.GenXRPLPubKey()
	xrplRelayer2PubKey := testutils.GenXRPLPubKey()
	xrplRelayer3PubKey := testutils.GenXRPLPubKey()

	xrplRelayer1Signature := testutils.GenXRPLSignature()
	xrplRelayer2Signature := testutils.GenXRPLSignature()

	coreumRelayer1Address := testutils.GenCoreumAccount()
	coreumRelayer2Address := testutils.GenCoreumAccount()
	coreumRelayer3Address := testutils.GenCoreumAccount()

	xrplSigners := []rippledata.Account{
		xrplRelayer1Account,
		xrplRelayer2Account,
		xrplRelayer3Account,
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
			XRPLAddress:   xrplRelayer1Account.String(),
			XRPLPubKey:    xrplRelayer1PubKey.String(),
		},
		{
			CoreumAddress: coreumRelayer2Address,
			XRPLAddress:   xrplRelayer2Account.String(),
			XRPLPubKey:    xrplRelayer2PubKey.String(),
		},
		{
			CoreumAddress: coreumRelayer3Address,
			XRPLAddress:   xrplRelayer3Account.String(),
			XRPLPubKey:    xrplRelayer3PubKey.String(),
		},
	}

	xrplBridgeSignerAccountWithSigners := xrpl.AccountInfoResult{
		AccountData: xrpl.AccountDataWithSigners{
			SignerList: []rippledata.SignerList{{
				SignerEntries: signerEntries,
				SignerQuorum:  &signerQuorum,
			}},
		},
	}

	allocateTicketOperationWithoutSignatures := coreum.Operation{
		SequenceNumber: 1,
		Signatures:     nil,
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: 3,
			},
		},
	}

	allocateTicketOperationWithSignatures := allocateTicketOperationWithoutSignatures
	allocateTicketOperationWithSignatures.Signatures = []coreum.Signature{
		{
			Relayer:   coreumRelayer1Address,
			Signature: xrplRelayer1Signature.String(),
		},
		{
			Relayer:   coreumRelayer2Address,
			Signature: xrplRelayer2Signature.String(),
		},
	}
	allocateTicketOperationWithSignaturesSigners := []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       xrplRelayer1Account,
				TxnSignature:  &xrplRelayer1Signature,
				SigningPubKey: &xrplRelayer1PubKey,
			},
		},
		{
			Signer: rippledata.SignerItem{
				Account:       xrplRelayer2Account,
				TxnSignature:  &xrplRelayer2Signature,
				SigningPubKey: &xrplRelayer2PubKey,
			},
		},
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
				contractClientMock.EXPECT().RegisterSignature(gomock.Any(), coreumRelayer1Address, allocateTicketOperationWithoutSignatures.SequenceNumber, xrplRelayer1Signature.String())
				return contractClientMock
			},
			xrplRPCClientBuilder: func(ctrl *gomock.Controller) processes.XRPLRPCClient {
				xrplRPCClientMock := NewMockXRPLRPCClient(ctrl)
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), xrplBridgeAccount).Return(xrplBridgeSignerAccountWithSigners, nil)
				return xrplRPCClientMock
			},
			xrplTxSignerBuilder: func(ctrl *gomock.Controller) processes.XRPLTxSigner {
				xrplTxSignerMock := NewMockXRPLTxSigner(ctrl)
				tx, err := processes.BuildTicketCreateTxForMultiSigning(xrplBridgeAccount, allocateTicketOperationWithoutSignatures)
				require.NoError(t, err)
				xrplTxSignerMock.EXPECT().MultiSign(tx, xrplTxSignerKeyName).Return(rippledata.Signer{
					Signer: rippledata.SignerItem{
						Account:       xrplRelayer1Account,
						TxnSignature:  &xrplRelayer1Signature,
						SigningPubKey: &xrplRelayer1PubKey,
					},
				}, nil)

				return xrplTxSignerMock
			},
		},
		{
			name: "submit_create_ticket_tx",
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
				xrplRPCClientMock.EXPECT().AccountInfo(gomock.Any(), xrplBridgeAccount).Return(xrplBridgeSignerAccountWithSigners, nil)
				expectedTx, err := processes.BuildTicketCreateTxForMultiSigning(xrplBridgeAccount, allocateTicketOperationWithSignatures)
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
					BridgeAccount:       xrplBridgeAccount,
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
