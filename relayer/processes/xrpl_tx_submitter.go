package processes

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// MultiSignableTransaction is XRPL multi-singable transaction type.
type MultiSignableTransaction interface {
	rippledata.MultiSignable
	rippledata.Transaction
}

// BridgeSigners full signers info.
type BridgeSigners struct {
	XRPLWeights         map[rippledata.Account]uint16
	XRPLWeightsQuorum   uint32
	XRPLPubKeys         map[rippledata.Account]rippledata.PublicKey
	CoreumToXRPLAccount map[string]rippledata.Account
}

// XRPLTxSubmitterConfig is the XRPLTxSubmitter config.
type XRPLTxSubmitterConfig struct {
	BridgeXRPLAddress    rippledata.Account
	RelayerCoreumAddress sdk.AccAddress
	XRPLTxSignerKeyName  string
	RepeatRecentScan     bool
	RepeatDelay          time.Duration
}

// DefaultXRPLTxSubmitterConfig returns the default XRPLTxSubmitter.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func DefaultXRPLTxSubmitterConfig(bridgeXRPLAddress rippledata.Account, relayerAddress sdk.AccAddress) XRPLTxSubmitterConfig {
	return XRPLTxSubmitterConfig{
		BridgeXRPLAddress:    bridgeXRPLAddress,
		RelayerCoreumAddress: relayerAddress,
		RepeatRecentScan:     true,
		RepeatDelay:          10 * time.Second,
	}
}

// XRPLTxSubmitter is process which observes pending XRPL operations, signs them and executes them.
type XRPLTxSubmitter struct {
	cfg            XRPLTxSubmitterConfig
	log            logger.Logger
	contractClient ContractClient
	xrplRPCClient  XRPLRPCClient
	xrplSigner     XRPLTxSigner
}

// NewXRPLTxSubmitter returns a new instance of the XRPLTxSubmitter.
func NewXRPLTxSubmitter(
	cfg XRPLTxSubmitterConfig,
	log logger.Logger,
	contractClient ContractClient,
	xrplRPCClient XRPLRPCClient,
	xrplSigner XRPLTxSigner,
) *XRPLTxSubmitter {
	return &XRPLTxSubmitter{
		cfg:            cfg,
		log:            log,
		contractClient: contractClient,
		xrplRPCClient:  xrplRPCClient,
		xrplSigner:     xrplSigner,
	}
}

// Init validates the process state.
func (s *XRPLTxSubmitter) Init(ctx context.Context) error {
	s.log.Debug(ctx, "Initializing process")

	if s.cfg.RelayerCoreumAddress.Empty() {
		return errors.Errorf("failed to init process, relayer address is nil or empty")
	}
	if !s.contractClient.IsInitialized() {
		return errors.Errorf("failed to init process, contract client is not initialized")
	}
	if s.xrplSigner == nil {
		return errors.Errorf("nil xrplSigner")
	}

	return nil
}

// Start starts the process.
func (s *XRPLTxSubmitter) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		default:
			if err := s.processPendingOperations(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error(ctx, "Failed to process pending operations", logger.Error(err))
			}
			if !s.cfg.RepeatRecentScan {
				s.log.Info(ctx, "Process repeating is disabled, process is finished")
				return nil
			}
			s.log.Info(ctx, "Waiting before the next execution", logger.StringField("delay", s.cfg.RepeatDelay.String()))
			select {
			case <-ctx.Done():
				return errors.WithStack(ctx.Err())
			case <-time.After(s.cfg.RepeatDelay):
			}
		}
	}
}

func (s *XRPLTxSubmitter) processPendingOperations(ctx context.Context) error {
	operations, err := s.contractClient.GetPendingOperations(ctx)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}

	bridgeSigners, err := s.getBridgeSigners(ctx)
	if err != nil {
		return err
	}

	for _, operation := range operations {
		if err := s.signOrSubmitOperation(ctx, operation, bridgeSigners); err != nil {
			return err
		}
	}

	return nil
}

//nolint:lll // TODO(dzmitryhil) linter length limit
func (s *XRPLTxSubmitter) getBridgeSigners(ctx context.Context) (BridgeSigners, error) {
	xrplWeights, xrplWeightsQuorum, err := s.getBridgeXRPLSignerAccountsWithWeights(ctx)
	if err != nil {
		return BridgeSigners{}, err
	}
	contractConfig, err := s.contractClient.GetContractConfig(ctx)
	if err != nil {
		return BridgeSigners{}, err
	}

	xrplPubKeys := make(map[rippledata.Account]rippledata.PublicKey, 0)
	coreumToXRPLAccount := make(map[string]rippledata.Account, 0)
	for _, relayer := range contractConfig.Relayers {
		xrplAcc, err := rippledata.NewAccountFromAddress(relayer.XRPLAddress)
		if err != nil {
			return BridgeSigners{}, errors.Wrapf(err, "failed to covert XRPL relayer address to Account type, address:%s", relayer.XRPLAddress)
		}
		var accPubKey rippledata.PublicKey
		if err := accPubKey.UnmarshalText([]byte(relayer.XRPLPubKey)); err != nil {
			return BridgeSigners{}, errors.Wrapf(err, "failed to unmarshal XRPL relayer pubkey, address:%s, pubKey:%s", relayer.XRPLAddress, relayer.XRPLPubKey)
		}

		xrplPubKeys[*xrplAcc] = accPubKey
		coreumToXRPLAccount[relayer.CoreumAddress.String()] = *xrplAcc
	}

	return BridgeSigners{
		XRPLWeights:         xrplWeights,
		XRPLWeightsQuorum:   xrplWeightsQuorum,
		XRPLPubKeys:         xrplPubKeys,
		CoreumToXRPLAccount: coreumToXRPLAccount,
	}, nil
}

//nolint:lll // TODO(dzmitryhil) linter length limit
func (s *XRPLTxSubmitter) getBridgeXRPLSignerAccountsWithWeights(ctx context.Context) (map[rippledata.Account]uint16, uint32, error) {
	accountInfo, err := s.xrplRPCClient.AccountInfo(ctx, s.cfg.BridgeXRPLAddress)
	if err != nil {
		return nil, 0, err
	}
	signerList := accountInfo.AccountData.SignerList
	if len(signerList) != 1 {
		return nil, 0, errors.Errorf("received unexpected length of the signer list")
	}
	signerData := accountInfo.AccountData.SignerList[0]
	weightsQuorum := *signerData.SignerQuorum
	accountWights := make(map[rippledata.Account]uint16, 0)
	for _, signerEntry := range signerData.SignerEntries {
		accountWights[*signerEntry.SignerEntry.Account] = *signerEntry.SignerEntry.SignerWeight
	}

	return accountWights, weightsQuorum, nil
}

//nolint:lll // TODO(dzmitryhil) linter length limit
func (s *XRPLTxSubmitter) signOrSubmitOperation(ctx context.Context, operation coreum.Operation, bridgeSigners BridgeSigners) error {
	valid, err := s.preValidateOperation(ctx, operation)
	if err != nil {
		return err
	}
	if !valid {
		s.log.Info(ctx, "Operation is invalid", logger.AnyField("operation", operation))
		return nil
	}

	tx, quorumIsReached, err := s.buildSubmittableTransaction(ctx, operation, bridgeSigners)
	if err != nil {
		return err
	}
	if !quorumIsReached {
		return s.registerTxSignature(ctx, operation)
	}

	txRes, err := s.xrplRPCClient.Submit(ctx, tx)
	if err != nil {
		return errors.Wrapf(err, "failed to submit transaction:%+v", tx)
	}
	if txRes.EngineResult.Success() {
		s.log.Info(ctx, "Transaction has been successfully submitted", logger.StringField("txHash", tx.GetHash().String()))
		return nil
	}

	switch txRes.EngineResult.String() {
	case xrpl.TefNOTicketTxResult, xrpl.TefPastSeqTxResult:
		s.log.Debug(ctx, "Transaction has been already submitted", logger.StringField("txHash", tx.GetHash().String()))
		return nil
	case xrpl.TecPathDryTxResult:
		s.log.Info(
			ctx,
			"The transaction has been sent, but will be reverted since the provided path does not have enough liquidity or the receipt doesn't link by trust lines.",
			logger.StringField("txHash", tx.GetHash().String()))
		return nil
	case xrpl.TecNoDstTxResult:
		s.log.Info(
			ctx,
			"The transaction has been sent, but will be reverted since account used in the transaction doesn't exist.",
			logger.StringField("txHash", tx.GetHash().String()))
		return nil
	case xrpl.TecInsufficientReserveTxResult:
		// for that case the tx will be accepted by the node and its rejection will be handled in the observer
		s.log.Error(ctx, "Insufficient reserve to complete the operation", logger.StringField("txHash", tx.GetHash().String()))
		return nil
	default:
		// TODO(dzmitryhil) handle the case when the keys are rotated but the bridgeSigners are from the previous state
		return errors.Errorf("failed to submit transaction, receveid unexpected result, result:%+v", txRes)
	}
}

//nolint:lll // TODO(dzmitryhil) linter length limit
func (s *XRPLTxSubmitter) buildSubmittableTransaction(
	ctx context.Context,
	operation coreum.Operation,
	bridgeSigners BridgeSigners,
) (MultiSignableTransaction, bool, error) {
	txSigners := make([]rippledata.Signer, 0)
	signedWeight := uint32(0)
	signingThresholdIsReached := false
	for _, signature := range operation.Signatures {
		xrplAcc, ok := bridgeSigners.CoreumToXRPLAccount[signature.RelayerCoreumAddress.String()]
		if !ok {
			s.log.Warn(ctx, "Found unknown signer", logger.StringField("coreumAddress", signature.RelayerCoreumAddress.String()))
			continue
		}
		xrplPubKey, ok := bridgeSigners.XRPLPubKeys[xrplAcc]
		if !ok {
			s.log.Warn(ctx, "Found XRPL signer address without pub key in the contract", logger.StringField("xrplAddress", xrplAcc.String()))
			continue
		}
		xrplAccWeight, ok := bridgeSigners.XRPLWeights[xrplAcc]
		if !ok {
			s.log.Warn(ctx, "Found XRPL signer address without weight", logger.StringField("xrplAddress", xrplAcc.String()))
			continue
		}
		var txSignature rippledata.VariableLength
		if err := txSignature.UnmarshalText([]byte(signature.Signature)); err != nil {
			s.log.Warn(
				ctx,
				"Failed to unmarshal tx signature",
				logger.Error(err),
				logger.StringField("signature", signature.Signature),
				logger.StringField("xrplAcc", xrplAcc.String()),
			)
			continue
		}
		txSigner := rippledata.Signer{
			Signer: rippledata.SignerItem{
				Account:       xrplAcc,
				TxnSignature:  &txSignature,
				SigningPubKey: &xrplPubKey,
			},
		}
		tx, err := s.buildXRPLTxFromOperation(operation)
		if err != nil {
			return nil, false, err
		}
		if err := rippledata.SetSigners(tx, txSigner); err != nil {
			return nil, false, errors.Errorf("failed to set tx signer, signer:%+v", txSigner)
		}
		isValid, _, err := rippledata.CheckMultiSignature(tx)
		if err != nil {
			s.log.Warn(ctx, "failed to check transaction signature, err:%s, signer:%+v", logger.Error(err), logger.AnyField("signer", txSigner))
			continue
		}
		if !isValid {
			s.log.Warn(
				ctx,
				"Invalid tx signature",
				logger.Error(err),
				logger.AnyField("txSigner", txSigner),
			)
			continue
		}
		txSigners = append(txSigners, txSigner)
		signedWeight += uint32(xrplAccWeight)
		// the fewer signatures we use the less fee we pay
		if signedWeight >= bridgeSigners.XRPLWeightsQuorum {
			signingThresholdIsReached = true
			break
		}
	}
	// quorum is not reached
	if !signingThresholdIsReached {
		return nil, false, nil
	}
	// build tx one more time to be sure that it is not affected
	tx, err := s.buildXRPLTxFromOperation(operation)
	if err != nil {
		return nil, false, err
	}
	if err := rippledata.SetSigners(tx, txSigners...); err != nil {
		return nil, false, errors.Errorf("failed to set tx signer, signeres:%+v", txSigners)
	}

	return tx, true, nil
}

// preValidateOperation checks if the operation is valid, and it makes sense to submit the corresponding transaction
// or the operation should be canceled with the `invalid` state. For now the main purpose of the function is to filter
// out the `AllocateTickets` operation with the invalid sequence.
func (s *XRPLTxSubmitter) preValidateOperation(ctx context.Context, operation coreum.Operation) (bool, error) {
	// no need to check if the current relayer has already provided the signature
	// this check prevents the state when relayer votes and then changes its vote because of different current state
	for _, signature := range operation.Signatures {
		if signature.RelayerCoreumAddress.String() == s.cfg.RelayerCoreumAddress.String() {
			return true, nil
		}
	}

	// currently we validate only the allocate tickets operation with not zero sequence
	if operation.OperationType.AllocateTickets == nil ||
		operation.OperationType.AllocateTickets.Number == 0 ||
		operation.AccountSequence == 0 {
		return true, nil
	}

	bridgeXRPLAccInfo, err := s.xrplRPCClient.AccountInfo(ctx, s.cfg.BridgeXRPLAddress)
	if err != nil {
		return false, err
	}
	// sequence is valid
	if *bridgeXRPLAccInfo.AccountData.Sequence == operation.AccountSequence {
		return true, nil
	}
	s.log.Info(
		ctx,
		"Invalid bridge account sequence",
		logger.Uint32Field("expected", *bridgeXRPLAccInfo.AccountData.Sequence),
		logger.Uint32Field("inOperation", operation.AccountSequence),
	)
	evidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TransactionResult: coreum.TransactionResultInvalid,
			AccountSequence:   lo.ToPtr(operation.AccountSequence),
			// we intentionally don't set the ticket number since it is unexpected to have invalid
			// tx with the ticket number
		},
	}
	s.log.Info(ctx, "Sending invalid tx evidence")
	_, err = s.contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, s.cfg.RelayerCoreumAddress, evidence)
	if err == nil {
		return false, nil
	}
	if IsEvidenceErrorCausedByResubmission(err) {
		s.log.Debug(ctx, "Received expected send evidence error")
		return false, nil
	}

	return false, nil
}

func (s *XRPLTxSubmitter) registerTxSignature(ctx context.Context, operation coreum.Operation) error {
	tx, err := s.buildXRPLTxFromOperation(operation)
	if err != nil {
		return err
	}
	signer, err := s.xrplSigner.MultiSign(tx, s.cfg.XRPLTxSignerKeyName)
	if err != nil {
		return errors.Wrapf(err, "failed to sign transaction, keyName:%s", s.cfg.XRPLTxSignerKeyName)
	}
	_, err = s.contractClient.SaveSignature(
		ctx,
		s.cfg.RelayerCoreumAddress,
		operation.GetOperationID(),
		signer.Signer.TxnSignature.String(),
	)
	if err == nil {
		return nil
	}
	if coreum.IsSignatureAlreadyProvidedError(err) {
		return nil
	}

	return errors.Wrap(err, "failed to register transaction signature")
}

func (s *XRPLTxSubmitter) buildXRPLTxFromOperation(operation coreum.Operation) (MultiSignableTransaction, error) {
	switch {
	case isAllocateTicketsOperation(operation):
		return BuildTicketCreateTxForMultiSigning(s.cfg.BridgeXRPLAddress, operation)
	case isTrustSetOperation(operation):
		return BuildTrustSetTxForMultiSigning(s.cfg.BridgeXRPLAddress, operation)
	case isCoreumToXRPLTransfer(operation):
		return BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning(s.cfg.BridgeXRPLAddress, operation)
	default:
		return nil, errors.Errorf("failed to process operation, unable to determine operation type, operation:%+v", operation)
	}
}

func isAllocateTicketsOperation(operation coreum.Operation) bool {
	return operation.OperationType.AllocateTickets != nil &&
		operation.OperationType.AllocateTickets.Number > 0
}

func isTrustSetOperation(operation coreum.Operation) bool {
	return operation.OperationType.TrustSet != nil &&
		operation.OperationType.TrustSet.Issuer != "" &&
		operation.OperationType.TrustSet.Currency != ""
}

func isCoreumToXRPLTransfer(operation coreum.Operation) bool {
	return operation.OperationType.CoreumToXRPLTransfer != nil &&
		operation.OperationType.CoreumToXRPLTransfer.Issuer != "" &&
		operation.OperationType.CoreumToXRPLTransfer.Currency != "" &&
		!operation.OperationType.CoreumToXRPLTransfer.Amount.IsZero() &&
		operation.OperationType.CoreumToXRPLTransfer.Recipient != ""
}
