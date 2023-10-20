package processes

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

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
	BridgeAccount       rippledata.Account
	RelayerAddress      sdk.AccAddress
	XRPLTxSignerKeyName string
	RepeatRecentScan    bool
	RepeatDelay         time.Duration
}

// DefaultXRPLTxSubmitterConfig returns the default XRPLTxSubmitter.
func DefaultXRPLTxSubmitterConfig(bridgeAccount rippledata.Account, relayerAddress sdk.AccAddress) XRPLTxSubmitterConfig {
	return XRPLTxSubmitterConfig{
		BridgeAccount:    bridgeAccount,
		RelayerAddress:   relayerAddress,
		RepeatRecentScan: true,
		RepeatDelay:      10 * time.Second,
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

	if s.cfg.RelayerAddress.Empty() {
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
			return ctx.Err()
		default:
			if err := s.processPendingOperations(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error(ctx, "Failed to process pending operations", logger.Error(err))
			}
			if !s.cfg.RepeatRecentScan {
				s.log.Info(ctx, "Process repeating is disabled, process is finished")
				return nil
			}
			s.log.Info(ctx, "Waiting before the next execution", logger.StringFiled("delay", s.cfg.RepeatDelay.String()))
			<-time.After(s.cfg.RepeatDelay)
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

func (s *XRPLTxSubmitter) getBridgeSigners(ctx context.Context) (BridgeSigners, error) {
	xrplWeights, xrplWeightsQuorum, err := s.getXRPLBridgeSignerAccountsWithWeights(ctx)
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

func (s *XRPLTxSubmitter) getXRPLBridgeSignerAccountsWithWeights(ctx context.Context) (map[rippledata.Account]uint16, uint32, error) {
	accountInfo, err := s.xrplRPCClient.AccountInfo(ctx, s.cfg.BridgeAccount)
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

func (s *XRPLTxSubmitter) signOrSubmitOperation(ctx context.Context, operation coreum.Operation, bridgeSigners BridgeSigners) error {
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
		s.log.Info(ctx, "Transaction successfully submitted", logger.StringFiled("txHash", tx.GetHash().String()))
		return nil
	}

	switch txRes.EngineResult.String() {
	case xrpl.TefNOTicketTxResult:
		s.log.Info(ctx, "Transaction was already submitted", logger.StringFiled("txHash", tx.GetHash().String()))
		return nil
	case xrpl.TefPastSeqTxResult, xrpl.TerPreSeqTxResult:
		s.log.Warn(ctx, "Used invalid sequence number", logger.Uint32Filed("sequence", tx.GetBase().Sequence))
		// TODO(dzmitryhil) cancel operation without the hash
		return nil
	default:
		// TODO(dzmitryhil) handle the case when the key are rotated but the bridgeSigners are from the previous state
		return errors.Errorf("failed to submit transaction, receveid unexpected result, result:%+v", txRes)
	}
}

func (s *XRPLTxSubmitter) buildSubmittableTransaction(
	ctx context.Context,
	operation coreum.Operation,
	bridgeSigners BridgeSigners,
) (MultiSignableTransaction, bool, error) {
	txSigners := make([]rippledata.Signer, 0)
	signedWeight := uint32(0)
	for _, signature := range operation.Signatures {
		xrplAcc, ok := bridgeSigners.CoreumToXRPLAccount[signature.Relayer.String()]
		if !ok {
			s.log.Warn(ctx, "Found unknown signer", logger.StringFiled("coreumAddress", signature.Relayer.String()))
			continue
		}
		xrplPubKey, ok := bridgeSigners.XRPLPubKeys[xrplAcc]
		if !ok {
			s.log.Warn(ctx, "Found XRPL signer address without pub key in the contract", logger.StringFiled("xrplAddress", xrplAcc.String()))
			continue
		}
		xrplAccWeight, ok := bridgeSigners.XRPLWeights[xrplAcc]
		if !ok {
			s.log.Warn(ctx, "Found XRPL signer address without weight", logger.StringFiled("xrplAddress", xrplAcc.String()))
			continue
		}
		tx, err := s.buildXRPLTxFromOperation(operation)
		if err != nil {
			return nil, false, err
		}

		var txSignature rippledata.VariableLength
		if err := txSignature.UnmarshalText([]byte(signature.Signature)); err != nil {
			// TODO(dzmitryhil) if the signature is invalid we use should use the kill switch
			s.log.Error(
				ctx,
				"Failed to unmarshal tx signature",
				logger.Error(err),
				logger.StringFiled("signature", signature.Signature),
				logger.StringFiled("xrplAcc", xrplAcc.String()),
			)
		}
		txSigner := rippledata.Signer{
			Signer: rippledata.SignerItem{
				Account:       xrplAcc,
				TxnSignature:  &txSignature,
				SigningPubKey: &xrplPubKey,
			},
		}
		txSigners = append(txSigners, txSigner)
		if err := rippledata.SetSigners(tx, txSigner); err != nil {
			return nil, false, errors.Errorf("failed set tx signer, signer:%+v", txSigner)
		}
		// TODO(dzmitryhil) validate signature here and filter out if invalid with warning
		signedWeight += uint32(xrplAccWeight)
	}
	// quorum is not reached
	if signedWeight < bridgeSigners.XRPLWeightsQuorum {
		return nil, false, nil
	}
	// build tx one more time to be sure that it is not affected
	tx, err := s.buildXRPLTxFromOperation(operation)
	if err != nil {
		return nil, false, err
	}
	if err := rippledata.SetSigners(tx, txSigners...); err != nil {
		return nil, false, errors.Errorf("failed set tx signer, signeres:%+v", txSigners)
	}

	return tx, true, nil
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
	_, err = s.contractClient.RegisterSignature(
		ctx,
		s.cfg.RelayerAddress,
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
	case operation.OperationType.AllocateTickets != nil && operation.OperationType.AllocateTickets.Number > 0:
		return BuildTicketCreateTxForMultiSigning(s.cfg.BridgeAccount, operation)
	default:
		return nil, errors.Errorf("failed to process operation, unable to determin operation type, operation:%+v", operation)
	}
}
