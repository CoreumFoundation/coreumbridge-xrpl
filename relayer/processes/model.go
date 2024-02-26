package processes

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

//go:generate mockgen -destination=model_mocks_test.go -package=processes_test . ContractClient,XRPLAccountTxScanner,XRPLRPCClient,XRPLTxSigner

// ContractClient is the interface for the contract client.
type ContractClient interface {
	IsInitialized() bool
	SendXRPLToCoreumTransferEvidence(
		ctx context.Context,
		sender sdk.AccAddress,
		evidence coreum.XRPLToCoreumTransferEvidence,
	) (*sdk.TxResponse, error)
	SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx context.Context,
		sender sdk.AccAddress,
		evidence coreum.XRPLTransactionResultTicketsAllocationEvidence,
	) (*sdk.TxResponse, error)
	SendXRPLTrustSetTransactionResultEvidence(
		ctx context.Context,
		sender sdk.AccAddress,
		evd coreum.XRPLTransactionResultTrustSetEvidence,
	) (*sdk.TxResponse, error)
	SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx context.Context,
		sender sdk.AccAddress,
		evd coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence,
	) (*sdk.TxResponse, error)
	SendKeysRotationTransactionResultEvidence(
		ctx context.Context,
		sender sdk.AccAddress,
		evd coreum.XRPLTransactionResultKeysRotationEvidence,
	) (*sdk.TxResponse, error)
	SaveSignature(
		ctx context.Context,
		sender sdk.AccAddress,
		operationID uint32,
		operationVersion uint32,
		signature string,
	) (*sdk.TxResponse, error)
	GetPendingOperations(ctx context.Context) ([]coreum.Operation, error)
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
}

// XRPLAccountTxScanner is XRPL account tx scanner.
type XRPLAccountTxScanner interface {
	ScanTxs(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error
}

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	AccountInfo(ctx context.Context, acc rippledata.Account) (xrpl.AccountInfoResult, error)
	Submit(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error)
}

// XRPLTxSigner is XRPL transaction signer.
type XRPLTxSigner interface {
	MultiSign(tx rippledata.MultiSignable, keyName string) (rippledata.Signer, error)
}

// IsExpectedEvidenceSubmissionError returns true is error is a part of expected business logic e.g:
// - error caused by tx resubmission;
// - maximum bridged amount reached;
// - token is not enabled at the moment of submission
// - etc.
func IsExpectedEvidenceSubmissionError(err error) bool {
	return coreum.IsEvidenceAlreadyProvidedError(err) ||
		coreum.IsOperationAlreadyExecutedError(err) ||
		coreum.IsPendingOperationNotFoundError(err) ||
		coreum.IsMaximumBridgedAmountReachedError(err) ||
		coreum.IsTokenNotEnabledError(err) ||
		coreum.IsProhibitedRecipientError(err) ||
		coreum.IsBridgeHaltedError(err) ||
		coreum.IsAmountSentIsZeroAfterTruncationError(err) ||
		coreum.IsCannotCoverBridgingFeesError(err)
}
