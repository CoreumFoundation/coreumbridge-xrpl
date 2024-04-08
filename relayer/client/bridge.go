//nolint:tagliatelle // yaml spec
package client

import (
	"context"
	"io"
	"os"
	"path/filepath"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	// the balance includes fee for the operations and some XRP on top to cover initial TrustSet txs.
	minBalanceToCoverFeeAndTrustLines = float64(20)
)

// ContractClient is the interface for the contract client.
//
//nolint:interfacebloat
type ContractClient interface {
	DeployAndInstantiate(
		ctx context.Context,
		sender sdk.AccAddress,
		byteCode []byte,
		config coreum.InstantiationConfig,
	) (sdk.AccAddress, error)
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
	GetContractOwnership(ctx context.Context) (coreum.ContractOwnership, error)
	RecoverTickets(
		ctx context.Context,
		sender sdk.AccAddress,
		accountSequence uint32,
		numberOfTickets *uint32,
	) (*sdk.TxResponse, error)
	RegisterCoreumToken(
		ctx context.Context,
		sender sdk.AccAddress,
		denom string,
		decimals uint32,
		sendingPrecision int32,
		maxHoldingAmount sdkmath.Int,
		bridgingFee sdkmath.Int,
	) (*sdk.TxResponse, error)
	RegisterXRPLToken(
		ctx context.Context,
		sender sdk.AccAddress,
		issuer, currency string,
		sendingPrecision int32,
		maxHoldingAmount sdkmath.Int,
		bridgingFee sdkmath.Int,
	) (*sdk.TxResponse, error)
	GetCoreumTokenByDenom(ctx context.Context, denom string) (coreum.CoreumToken, error)
	GetCoreumTokens(ctx context.Context) ([]coreum.CoreumToken, error)
	GetXRPLTokens(ctx context.Context) ([]coreum.XRPLToken, error)
	GetXRPLTokenByIssuerAndCurrency(ctx context.Context, issuer, currency string) (coreum.XRPLToken, error)
	SendToXRPL(
		ctx context.Context,
		sender sdk.AccAddress,
		recipient string,
		amount sdk.Coin,
		deliverAmount *sdkmath.Int,
	) (*sdk.TxResponse, error)
	UpdateXRPLToken(
		ctx context.Context,
		sender sdk.AccAddress,
		issuer, currency string,
		state *coreum.TokenState,
		sendingPrecision *int32,
		maxHoldingAmount *sdkmath.Int,
		bridgingFee *sdkmath.Int,
	) (*sdk.TxResponse, error)
	UpdateCoreumToken(
		ctx context.Context,
		sender sdk.AccAddress,
		denom string,
		state *coreum.TokenState,
		sendingPrecision *int32,
		maxHoldingAmount *sdkmath.Int,
		bridgingFee *sdkmath.Int,
	) (*sdk.TxResponse, error)
	GetPendingRefunds(ctx context.Context, address sdk.AccAddress) ([]coreum.PendingRefund, error)
	ClaimRefund(
		ctx context.Context,
		sender sdk.AccAddress,
		pendingRefundID string,
	) (*sdk.TxResponse, error)
	GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error)
	ClaimRelayerFees(
		ctx context.Context,
		sender sdk.AccAddress,
		amounts sdk.Coins,
	) (*sdk.TxResponse, error)
	RotateKeys(
		ctx context.Context,
		sender sdk.AccAddress,
		newRelayers []coreum.Relayer,
		newEvidenceThreshold uint32,
	) (*sdk.TxResponse, error)
	RecoverXRPLTokenRegistration(
		ctx context.Context,
		sender sdk.AccAddress,
		issuer, currency string,
	) (*sdk.TxResponse, error)
	HaltBridge(
		ctx context.Context,
		sender sdk.AccAddress,
	) (*sdk.TxResponse, error)
	ResumeBridge(
		ctx context.Context,
		sender sdk.AccAddress,
	) (*sdk.TxResponse, error)
	UpdateXRPLBaseFee(
		ctx context.Context,
		sender sdk.AccAddress,
		xrplBaseFee uint32,
	) (*sdk.TxResponse, error)
	GetProhibitedXRPLAddresses(ctx context.Context) ([]string, error)
	UpdateProhibitedXRPLAddresses(
		ctx context.Context,
		sender sdk.AccAddress,
		prohibitedXRPLAddresses []string,
	) (*sdk.TxResponse, error)
	CancelPendingOperation(
		ctx context.Context,
		sender sdk.AccAddress,
		operationID uint32,
	) (*sdk.TxResponse, error)
	GetPendingOperations(ctx context.Context) ([]coreum.Operation, error)
	GetTransactionEvidences(ctx context.Context) ([]coreum.TransactionEvidence, error)
	DeployContract(
		ctx context.Context,
		sender sdk.AccAddress,
		byteCode []byte,
	) (*sdk.TxResponse, uint64, error)
	MigrateContract(
		ctx context.Context,
		sender sdk.AccAddress,
		codeID uint64,
	) (*sdk.TxResponse, error)
	GetXRPLToCoreumTracingInfo(
		ctx context.Context,
		xrplTxHash string,
	) (coreum.XRPLToCoreumTracingInfo, error)
	GetCoreumToXRPLTracingInfo(
		ctx context.Context,
		coreumTxHash string,
	) (coreum.CoreumToXRPLTracingInfo, error)
}

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	AccountInfo(ctx context.Context, acc rippledata.Account) (xrpl.AccountInfoResult, error)
	AutoFillTx(
		ctx context.Context,
		tx rippledata.Transaction,
		sender rippledata.Account,
		multiSigningSignatureCount uint32,
	) error
	Submit(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error)
	SubmitAndAwaitSuccess(ctx context.Context, tx rippledata.Transaction) error
	AccountLines(
		ctx context.Context,
		account rippledata.Account,
		ledgerIndex any,
		marker string,
	) (xrpl.AccountLinesResult, error)
	GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error)
	Tx(ctx context.Context, hash rippledata.Hash256) (xrpl.TxResult, error)
}

// XRPLTxSigner is XRPL transaction signer.
type XRPLTxSigner interface {
	Account(keyName string) (rippledata.Account, error)
	Sign(tx rippledata.Transaction, keyName string) error
}

// RelayerConfig is relayer config used for the bootstrapping and keys rotation.
type RelayerConfig struct {
	CoreumAddress string `yaml:"coreum_address"`
	XRPLAddress   string `yaml:"xrpl_address"`
	XRPLPubKey    string `yaml:"xrpl_pub_key"`
}

// BootstrappingConfig the struct contains the setting for the bridge XRPL account creation and contract deployment.
type BootstrappingConfig struct {
	Owner                       string          `yaml:"owner"`
	Admin                       string          `yaml:"admin"`
	Relayers                    []RelayerConfig `yaml:"relayers"`
	EvidenceThreshold           uint32          `yaml:"evidence_threshold"`
	UsedTicketSequenceThreshold uint32          `yaml:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         string          `yaml:"trust_set_limit_amount"`
	ContractByteCodePath        string          `yaml:"contract_bytecode_path"`
	XRPLBaseFee                 uint32          `yaml:"xrpl_base_fee"`
	SkipXRPLBalanceValidation   bool            `yaml:"-"`
}

// DefaultBootstrappingConfig returns default BootstrappingConfig.
func DefaultBootstrappingConfig() BootstrappingConfig {
	return BootstrappingConfig{
		Owner:                       "",
		Admin:                       "",
		Relayers:                    []RelayerConfig{{}},
		EvidenceThreshold:           0,
		UsedTicketSequenceThreshold: 150,
		// default trust set limit amount is close to max amount valid on both XRPL and Coreum chains
		TrustSetLimitAmount:       sdkmath.NewIntWithDecimal(34, 37).String(),
		ContractByteCodePath:      "",
		SkipXRPLBalanceValidation: false,
		XRPLBaseFee:               xrpl.DefaultXRPLBaseFee,
	}
}

// KeysRotationConfig the struct contains the setting for the keys rotation.
type KeysRotationConfig struct {
	Relayers          []RelayerConfig `yaml:"relayers"`
	EvidenceThreshold uint32          `yaml:"evidence_threshold"`
}

// DefaultKeysRotationConfig return default KeysRotationConfig.
func DefaultKeysRotationConfig() KeysRotationConfig {
	return KeysRotationConfig{
		Relayers: []RelayerConfig{
			// keep one empty relayer for a template
			{},
		},
		EvidenceThreshold: 0,
	}
}

// XRPLToCoreumTracingInfo is XRPL to Coreum tracing info.
type XRPLToCoreumTracingInfo struct {
	XRPLTx        rippledata.TransactionWithMetaData
	CoreumTx      *sdk.TxResponse
	EvidenceToTxs []coreum.DataToTx[coreum.XRPLToCoreumTransferEvidence]
}

// CoreumToXRPLTracingInfo is Coreum to XRPL tracing info.
type CoreumToXRPLTracingInfo struct {
	CoreumTx      sdk.TxResponse
	XRPLTx        *rippledata.TransactionWithMetaData
	EvidenceToTxs []coreum.DataToTx[coreum.XRPLTransactionResultEvidence]
}

// BridgeClient is the service responsible for the bridge bootstrapping.
type BridgeClient struct {
	log             logger.Logger
	coreumClientCtx client.Context
	contractClient  ContractClient
	xrplRPCClient   XRPLRPCClient
	xrplTxSigner    XRPLTxSigner
}

// NewBridgeClient returns a new instance of the BridgeClient.
func NewBridgeClient(
	log logger.Logger,
	coreumClientCtx client.Context,
	contractClient ContractClient,
	xrplRPCClient XRPLRPCClient,
	xrplTxSigner XRPLTxSigner,
) *BridgeClient {
	return &BridgeClient{
		log:             log,
		coreumClientCtx: coreumClientCtx,
		contractClient:  contractClient,
		xrplRPCClient:   xrplRPCClient,
		xrplTxSigner:    xrplTxSigner,
	}
}

// Bootstrap creates initial XRPL bridge multi-signing account with the disabled master key,
// enabled rippling on it, and deploys the bridge contract with the provided settings.
func (b *BridgeClient) Bootstrap(
	ctx context.Context,
	senderAddress sdk.AccAddress,
	bridgeAccountKeyName string,
	cfg BootstrappingConfig,
) (sdk.AccAddress, error) {
	xrplBridgeAccount, err := b.xrplTxSigner.Account(bridgeAccountKeyName)
	if err != nil {
		return nil, err
	}
	b.log.Info(
		ctx,
		"XRPL account details",
		zap.String("keyName", bridgeAccountKeyName),
		zap.String("xrplAddress", xrplBridgeAccount.String()),
	)
	if !cfg.SkipXRPLBalanceValidation {
		if err = b.validateXRPLBridgeAccountBalance(ctx, xrplBridgeAccount); err != nil {
			return nil, err
		}
	}
	// validate the config and fill required objects
	relayers, xrplSignerEntries, err := b.buildContractRelayersFromRelayersConfig(ctx, cfg.Relayers)
	if err != nil {
		return nil, err
	}
	// prepare deployment config
	contactByteCode, err := os.ReadFile(cfg.ContractByteCodePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get contract bytecode by path:%s", cfg.ContractByteCodePath)
	}
	owner, err := sdk.AccAddressFromBech32(cfg.Owner)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse owner")
	}
	admin, err := sdk.AccAddressFromBech32(cfg.Admin)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse admin")
	}
	trustSetLimitAmount, ok := sdkmath.NewIntFromString(cfg.TrustSetLimitAmount)
	if !ok {
		return nil,
			errors.Wrapf(
				err,
				"failed to convert trustSetLimitAmount to sdkmth.Int, trustSetLimitAmount:%s",
				trustSetLimitAmount,
			)
	}
	instantiationCfg := coreum.InstantiationConfig{
		Owner:                       owner,
		Admin:                       admin,
		Relayers:                    relayers,
		EvidenceThreshold:           cfg.EvidenceThreshold,
		UsedTicketSequenceThreshold: cfg.UsedTicketSequenceThreshold,
		TrustSetLimitAmount:         trustSetLimitAmount,
		BridgeXRPLAddress:           xrplBridgeAccount.String(),
		XRPLBaseFee:                 cfg.XRPLBaseFee,
	}
	b.log.Info(ctx, "Deploying contract", zap.Any("settings", instantiationCfg))
	contractAddress, err := b.contractClient.DeployAndInstantiate(ctx, senderAddress, contactByteCode, instantiationCfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy contract")
	}
	b.log.Info(ctx, "Contract is deployed successfully", zap.String("address", contractAddress.String()))

	if err := b.setUpXRPLBridgeAccount(ctx, bridgeAccountKeyName, cfg, xrplSignerEntries); err != nil {
		return nil, err
	}

	b.log.Info(ctx, "The XRPL bridge account is ready", zap.String("address", xrplBridgeAccount.String()))
	return contractAddress, nil
}

// DeployContract deploys smart contract.
func (b *BridgeClient) DeployContract(
	ctx context.Context,
	sender sdk.AccAddress,
	contractByteCodePath string,
) (*sdk.TxResponse, uint64, error) {
	b.log.Info(
		ctx,
		"Deploying contract",
		zap.String("path", contractByteCodePath),
	)

	contactByteCode, err := os.ReadFile(contractByteCodePath)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get contract bytecode by path:%s", contractByteCodePath)
	}

	txRes, codeID, err := b.contractClient.DeployContract(ctx, sender, contactByteCode)
	if err != nil {
		return nil, 0, err
	}

	if txRes == nil {
		return nil, 0, nil
	}

	b.log.Info(
		ctx,
		"Successfully deployed contract",
		zap.Uint64("codeID", codeID),
	)

	return txRes, codeID, nil
}

// GetContractConfig returns contract config.
func (b *BridgeClient) GetContractConfig(ctx context.Context) (coreum.ContractConfig, error) {
	return b.contractClient.GetContractConfig(ctx)
}

// GetContractOwnership returns contract ownership.
func (b *BridgeClient) GetContractOwnership(ctx context.Context) (coreum.ContractOwnership, error) {
	return b.contractClient.GetContractOwnership(ctx)
}

// RecoverTickets recovers tickets allocation.
func (b *BridgeClient) RecoverTickets(
	ctx context.Context,
	ownerAddress sdk.AccAddress,
	ticketsToAllocate *uint32,
) error {
	logFields := make([]zap.Field, 0)
	if ticketsToAllocate != nil {
		logFields = append(logFields, zap.Uint32("numberOfTickets", *ticketsToAllocate))
	}
	b.log.Info(ctx, "Recovering tickets", logFields...)
	cfg, err := b.contractClient.GetContractConfig(ctx)
	if err != nil {
		return err
	}
	bridgeXRPLAddress, err := rippledata.NewAccountFromAddress(cfg.BridgeXRPLAddress)
	if err != nil {
		return errors.Wrapf(
			err,
			"failed to convert BridgeXRPLAddress from contract to rippledata.Account, address:%s",
			cfg.BridgeXRPLAddress,
		)
	}
	b.log.Info(ctx, "Getting bridge account sequence", zap.String("address", cfg.BridgeXRPLAddress))
	accInfo, err := b.xrplRPCClient.AccountInfo(ctx, *bridgeXRPLAddress)
	if err != nil {
		return err
	}
	b.log.Info(ctx, "Got bridge account sequence", zap.Uint32("sequence", *accInfo.AccountData.Sequence))
	txRes, err := b.contractClient.RecoverTickets(
		ctx,
		ownerAddress,
		*accInfo.AccountData.Sequence,
		ticketsToAllocate,
	)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully submitted recovery tickets transaction",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// RegisterCoreumToken registers Coreum token.
func (b *BridgeClient) RegisterCoreumToken(
	ctx context.Context,
	owner sdk.AccAddress,
	denom string,
	decimals uint32,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) (coreum.CoreumToken, error) {
	b.log.Info(
		ctx,
		"Registering Coreum token",
		zap.String("owner", owner.String()),
		zap.String("denom", denom),
		zap.Uint32("decimals", decimals),
		zap.Int32("sendingPrecision", sendingPrecision),
		zap.String("maxHoldingAmount", maxHoldingAmount.String()),
		zap.String("bridgingFee", bridgingFee.String()),
	)
	txRes, err := b.contractClient.RegisterCoreumToken(
		ctx,
		owner,
		denom,
		decimals,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
	if err != nil {
		return coreum.CoreumToken{}, err
	}

	if txRes == nil {
		return coreum.CoreumToken{}, nil
	}

	token, err := b.contractClient.GetCoreumTokenByDenom(ctx, denom)
	if err != nil {
		return coreum.CoreumToken{}, err
	}
	b.log.Info(
		ctx,
		"Successfully registered Coreum token",
		zap.Any("token", token),
		zap.String("txHash", txRes.TxHash),
	)

	return token, nil
}

// RegisterXRPLToken registers XRPL token.
func (b *BridgeClient) RegisterXRPLToken(
	ctx context.Context,
	owner sdk.AccAddress,
	issuer rippledata.Account, currency rippledata.Currency,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) (coreum.XRPLToken, error) {
	stringCurrency := xrpl.ConvertCurrencyToString(currency)
	b.log.Info(
		ctx,
		"Registering XRPL token",
		zap.String("owner", owner.String()),
		zap.String("issuer", issuer.String()),
		zap.String("currency", stringCurrency),
		zap.Int32("sendingPrecision", sendingPrecision),
		zap.String("maxHoldingAmount", maxHoldingAmount.String()),
		zap.String("bridgingFee", bridgingFee.String()),
	)
	txRes, err := b.contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer.String(),
		stringCurrency,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
	if err != nil {
		return coreum.XRPLToken{}, err
	}

	if txRes == nil {
		return coreum.XRPLToken{}, nil
	}

	token, err := b.contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer.String(), stringCurrency)
	if err != nil {
		return coreum.XRPLToken{}, err
	}

	b.log.Info(
		ctx,
		"Successfully registered XRPL token",
		zap.Any("token", token),
		zap.String("txHash", txRes.TxHash),
	)

	return token, nil
}

// RecoverXRPLTokenRegistration recovers xrpl token registration.
func (b *BridgeClient) RecoverXRPLTokenRegistration(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
) error {
	b.log.Info(ctx, "Recovering xrpl token registration",
		zap.String("currency", currency),
		zap.String("issuer", issuer),
	)
	txRes, err := b.contractClient.RecoverXRPLTokenRegistration(
		ctx,
		sender,
		issuer,
		currency,
	)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}
	b.log.Info(
		ctx,
		"Recovering xrpl token registration was successful",
		zap.String("currency", currency),
		zap.String("issuer", issuer),
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// GetAllTokens returns all registered tokens.
func (b *BridgeClient) GetAllTokens(ctx context.Context) ([]coreum.CoreumToken, []coreum.XRPLToken, error) {
	coreumTokens, err := b.contractClient.GetCoreumTokens(ctx)
	if err != nil {
		return nil, nil, err
	}

	xrplTokens, err := b.contractClient.GetXRPLTokens(ctx)
	if err != nil {
		return nil, nil, err
	}

	return coreumTokens, xrplTokens, nil
}

// GetXRPLTokenByIssuerAndCurrency returns XRPL registered token by issuer and currency.
func (b *BridgeClient) GetXRPLTokenByIssuerAndCurrency(
	ctx context.Context,
	issuer, currency string,
) (coreum.XRPLToken, error) {
	return b.contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
}

// SendFromCoreumToXRPL sends tokens form Coreum to XRPL.
func (b *BridgeClient) SendFromCoreumToXRPL(
	ctx context.Context,
	sender sdk.AccAddress,
	recipient rippledata.Account,
	amount sdk.Coin,
	deliverAmount *sdkmath.Int,
) (string, error) {
	logFields := []zap.Field{
		zap.String("sender", sender.String()),
		zap.String("amount", amount.String()),
		zap.String("recipient", recipient.String()),
	}
	if deliverAmount != nil {
		logFields = append(logFields, zap.String("deliverAmount", deliverAmount.String()))
	}
	b.log.Info(
		ctx,
		"Sending tokens form Coreum to XRPL",
		logFields...,
	)
	txRes, err := b.contractClient.SendToXRPL(ctx, sender, recipient.String(), amount, deliverAmount)
	if err != nil {
		return "", err
	}

	if txRes == nil {
		return "", nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to send from Coreum to XRPL",
		zap.String("txHash", txRes.TxHash),
	)

	return txRes.TxHash, nil
}

// SendFromXRPLToCoreum sends tokens form XRPL to Coreum.
func (b *BridgeClient) SendFromXRPLToCoreum(
	ctx context.Context,
	senderKeyName string,
	amount rippledata.Amount,
	recipient sdk.AccAddress,
) (string, error) {
	senderAccount, err := b.xrplTxSigner.Account(senderKeyName)
	if err != nil {
		return "", err
	}

	b.log.Info(
		ctx,
		"Sending tokens form XRPL to Coreum",
		zap.String("sender", senderAccount.String()),
		zap.String("amount", amount.String()),
		zap.String("recipient", recipient.String()),
	)

	cfg, err := b.contractClient.GetContractConfig(ctx)
	if err != nil {
		return "", err
	}
	xrplBridgeAddress, err := rippledata.NewAccountFromAddress(cfg.BridgeXRPLAddress)
	if err != nil {
		return "", errors.Wrapf(
			err,
			"failed to convert BridgeXRPLAddress from contract to rippledata.Account, address:%s",
			cfg.BridgeXRPLAddress,
		)
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(recipient)
	if err != nil {
		return "", err
	}

	paymentTx := rippledata.Payment{
		Destination: *xrplBridgeAddress,
		Amount:      amount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
		},
	}

	return b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &paymentTx, senderKeyName)
}

// SetXRPLTrustSet sends XRPL TrustSet transaction.
func (b *BridgeClient) SetXRPLTrustSet(
	ctx context.Context,
	senderKeyName string,
	limitAmount rippledata.Amount,
) error {
	senderAccount, err := b.xrplTxSigner.Account(senderKeyName)
	if err != nil {
		return err
	}

	b.log.Info(
		ctx,
		"Sending XRPL TrustSet",
		zap.String("sender", senderAccount.String()),
		zap.String("limitAmount", limitAmount.String()),
	)

	trustSetTx := rippledata.TrustSet{
		LimitAmount: limitAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
			Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
		},
	}

	if _, err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &trustSetTx, senderKeyName); err != nil {
		return err
	}

	return nil
}

// UpdateCoreumToken updates Coreum token.
func (b *BridgeClient) UpdateCoreumToken(
	ctx context.Context,
	sender sdk.AccAddress,
	denom string,
	state *coreum.TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) error {
	fields := []zap.Field{
		zap.String("sender", sender.String()),
		zap.String("denom", denom),
	}
	if state != nil {
		fields = append(fields, zap.String("state", string(*state)))
	}
	if sendingPrecision != nil {
		fields = append(fields, zap.Int32("sendingPrecision", *sendingPrecision))
	}
	if maxHoldingAmount != nil {
		fields = append(fields, zap.String("maxHoldingAmount", maxHoldingAmount.String()))
	}
	if bridgingFee != nil {
		fields = append(fields, zap.String("bridgingFee", bridgingFee.String()))
	}
	b.log.Info(
		ctx,
		"Updating token",
		fields...,
	)

	txRes, err := b.contractClient.UpdateCoreumToken(
		ctx, sender, denom, state, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to update Coreum token",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// UpdateXRPLToken updates XRPL token state.
func (b *BridgeClient) UpdateXRPLToken(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
	state *coreum.TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) error {
	fields := []zap.Field{
		zap.String("sender", sender.String()),
		zap.String("issuer", issuer),
		zap.String("currency", currency),
	}
	if state != nil {
		fields = append(fields, zap.String("state", string(*state)))
	}
	if sendingPrecision != nil {
		fields = append(fields, zap.Int32("sendingPrecision", *sendingPrecision))
	}
	if maxHoldingAmount != nil {
		fields = append(fields, zap.String("maxHoldingAmount", maxHoldingAmount.String()))
	}
	if bridgingFee != nil {
		fields = append(fields, zap.String("bridgingFee", bridgingFee.String()))
	}
	b.log.Info(
		ctx,
		"Updating token",
		fields...,
	)
	txRes, err := b.contractClient.UpdateXRPLToken(
		ctx, sender, issuer, currency, state, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to update XRPL token",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// ResumeBridge resumes bridge after the halt.
func (b *BridgeClient) ResumeBridge(
	ctx context.Context,
	sender sdk.AccAddress,
) error {
	b.log.Info(
		ctx,
		"Resuming bridge",
	)

	txRes, err := b.contractClient.ResumeBridge(ctx, sender)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to resume bridge",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// RotateKeys start bridge keys rotation process.
func (b *BridgeClient) RotateKeys(
	ctx context.Context,
	sender sdk.AccAddress,
	cfg KeysRotationConfig,
) error {
	b.log.Info(
		ctx,
		"Rotating Keys",
		zap.Any("cfg", cfg),
	)

	relayers, _, err := b.buildContractRelayersFromRelayersConfig(ctx, cfg.Relayers)
	if err != nil {
		return err
	}

	txRes, err := b.contractClient.RotateKeys(ctx, sender, relayers, cfg.EvidenceThreshold)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to rotate keys",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// UpdateXRPLBaseFee updates the XRPL base fee for the contract.
func (b *BridgeClient) UpdateXRPLBaseFee(
	ctx context.Context,
	sender sdk.AccAddress,
	xrplBaseFee uint32,
) error {
	b.log.Info(
		ctx,
		"Updating XRPL base fee",
		zap.Uint32("xrplBaseFee", xrplBaseFee),
	)

	txRes, err := b.contractClient.UpdateXRPLBaseFee(ctx, sender, xrplBaseFee)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to update XRPL base fee",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// GetFeesCollected returns the fees collected by a relayer.
func (b *BridgeClient) GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error) {
	return b.contractClient.GetFeesCollected(ctx, address)
}

// ClaimRelayerFees calls the contract to claim the fees for a given relayer.
func (b *BridgeClient) ClaimRelayerFees(
	ctx context.Context,
	sender sdk.AccAddress,
	amounts sdk.Coins,
) error {
	b.log.Info(
		ctx,
		"Claiming relayer fees",
		zap.Any("amounts", amounts),
	)

	txRes, err := b.contractClient.ClaimRelayerFees(ctx, sender, amounts)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully claimed relayer fees",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// GetCoreumBalances returns all coreum account balances.
func (b *BridgeClient) GetCoreumBalances(ctx context.Context, address sdk.AccAddress) (sdk.Coins, error) {
	bankClient := banktypes.NewQueryClient(b.coreumClientCtx)
	res, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
		Address: address.String(),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get coreum balances, address:%s", address.String())
	}

	return res.Balances, nil
}

// GetXRPLBalances returns all XRPL account balances.
func (b *BridgeClient) GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error) {
	return b.xrplRPCClient.GetXRPLBalances(ctx, acc)
}

// GetPendingRefunds queries for the pending refunds of an address.
func (b *BridgeClient) GetPendingRefunds(ctx context.Context, address sdk.AccAddress) ([]coreum.PendingRefund, error) {
	b.log.Info(ctx, "Getting pending refunds", zap.String("address", address.String()))
	return b.contractClient.GetPendingRefunds(ctx, address)
}

// ClaimRefund claims pending refund.
func (b *BridgeClient) ClaimRefund(ctx context.Context, address sdk.AccAddress, refundID string) error {
	b.log.Info(ctx, "Claiming pending refund",
		zap.String("address", address.String()),
		zap.String("refundID", refundID))
	txRes, err := b.contractClient.ClaimRefund(ctx, address, refundID)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(ctx, "Finished execution of claiming pending refund",
		zap.String("address", address.String()),
		zap.String("refundID", refundID),
		zap.String("txHash", txRes.TxHash),
	)
	return nil
}

// GetProhibitedXRPLAddresses queries for the list of the prohibited XRPL addresses.
func (b *BridgeClient) GetProhibitedXRPLAddresses(ctx context.Context) ([]string, error) {
	return b.contractClient.GetProhibitedXRPLAddresses(ctx)
}

// UpdateProhibitedXRPLAddresses updates the list of the prohibited XRPL addresses.
func (b *BridgeClient) UpdateProhibitedXRPLAddresses(
	ctx context.Context, address sdk.AccAddress, prohibitedXRPLAddresses []string,
) error {
	b.log.Info(ctx, "Updating prohibited XRPL addresses",
		zap.Any("prohibitedXRPLAddresses", prohibitedXRPLAddresses))

	txRes, err := b.contractClient.UpdateProhibitedXRPLAddresses(ctx, address, prohibitedXRPLAddresses)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(
		ctx,
		"Successfully updated prohibited XRPL addresses",
		zap.String("txHash", txRes.TxHash),
	)
	return nil
}

func (b *BridgeClient) buildContractRelayersFromRelayersConfig(
	ctx context.Context,
	relayers []RelayerConfig,
) ([]coreum.Relayer, []rippledata.SignerEntry, error) {
	coreumAuthClient := authtypes.NewQueryClient(b.coreumClientCtx)
	contractRelayers := make([]coreum.Relayer, 0, len(relayers))
	xrplSignerEntries := make([]rippledata.SignerEntry, 0)
	for _, relayer := range relayers {
		if _, err := coreumAuthClient.Account(ctx, &authtypes.QueryAccountRequest{
			Address: relayer.CoreumAddress,
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "failed to get coreum account by address:%s", relayer.CoreumAddress)
		}
		xrplAddress, err := rippledata.NewAccountFromAddress(relayer.XRPLAddress)
		if err != nil {
			return nil, nil, errors.Wrapf(
				err,
				"failed to convert XRPL address string to rippledata.Account, address:%s",
				relayer.XRPLAddress,
			)
		}
		xrplAccInfo, err := b.xrplRPCClient.AccountInfo(ctx, *xrplAddress)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to get XRPL account by address:%s", xrplAddress.String())
		}
		if xrplAccInfo.AccountData.Balance.Float() < xrpl.ReserveToActivateAccount {
			return nil, nil, errors.Errorf(
				"insufficient XRPL relayer account balance, required:%f, current:%f",
				xrpl.ReserveToActivateAccount, xrplAccInfo.AccountData.Balance.Float(),
			)
		}
		relayerCoreumAddress, err := sdk.AccAddressFromBech32(relayer.CoreumAddress)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to parse relayerCoreumAddress:%s", relayer.CoreumAddress)
		}
		contractRelayers = append(contractRelayers, coreum.Relayer{
			CoreumAddress: relayerCoreumAddress,
			XRPLAddress:   relayer.XRPLAddress,
			XRPLPubKey:    relayer.XRPLPubKey,
		})
		xrplSignerEntries = append(xrplSignerEntries, rippledata.SignerEntry{
			SignerEntry: rippledata.SignerEntryItem{
				Account:      xrplAddress,
				SignerWeight: lo.ToPtr(uint16(1)),
			},
		})
	}

	return contractRelayers, xrplSignerEntries, nil
}

// HaltBridge halts the bridge.
func (b *BridgeClient) HaltBridge(
	ctx context.Context,
	sender sdk.AccAddress,
) error {
	b.log.Info(ctx, "Halting the bridge", zap.String("sender", sender.String()))
	txRes, err := b.contractClient.HaltBridge(ctx, sender)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(ctx, "The bridge is halted", zap.String("txHash", txRes.TxHash))
	return nil
}

// CancelPendingOperation executes `cancel_pending_operation` method.
func (b *BridgeClient) CancelPendingOperation(
	ctx context.Context,
	sender sdk.AccAddress,
	operationID uint32,
) error {
	b.log.Info(ctx, "Cancelling pending operation", zap.Uint32("operationID", operationID))
	txRes, err := b.contractClient.CancelPendingOperation(ctx, sender, operationID)
	if err != nil {
		return err
	}

	if txRes == nil {
		return nil
	}

	b.log.Info(ctx, "finished execution of the cancelling pending operation", zap.String("txHash", txRes.TxHash))
	return nil
}

// GetPendingOperations returns a list of all pending operations.
func (b *BridgeClient) GetPendingOperations(ctx context.Context) ([]coreum.Operation, error) {
	b.log.Info(ctx, "Getting pending operations")
	return b.contractClient.GetPendingOperations(ctx)
}

// GetTransactionEvidences returns a list of not confirmed transaction evidences.
func (b *BridgeClient) GetTransactionEvidences(ctx context.Context) ([]coreum.TransactionEvidence, error) {
	b.log.Info(ctx, "Getting transaction evidences")
	return b.contractClient.GetTransactionEvidences(ctx)
}

// GetXRPLToCoreumTracingInfo returns XRPL to Coreum tracing info.
func (b *BridgeClient) GetXRPLToCoreumTracingInfo(
	ctx context.Context,
	xrplTxHash string,
) (XRPLToCoreumTracingInfo, error) {
	b.log.Info(ctx, "Getting XRPL to Coreum transfer tracing info")
	xrplHash, err := rippledata.NewHash256(xrplTxHash)
	if err != nil {
		return XRPLToCoreumTracingInfo{}, errors.Wrapf(err, "invalid XRPL tx hash:%s", xrplTxHash)
	}

	tx, err := b.xrplRPCClient.Tx(ctx, *xrplHash)
	if err != nil {
		return XRPLToCoreumTracingInfo{}, err
	}

	if tx.GetType() != rippledata.PAYMENT.String() {
		return XRPLToCoreumTracingInfo{}, errors.Errorf(
			"invalid XRPL transaction type, expected %s, got: %s", rippledata.PAYMENT.String(), tx.GetType(),
		)
	}
	paymentTx, ok := tx.Transaction.(*rippledata.Payment)
	if !ok {
		return XRPLToCoreumTracingInfo{}, errors.Errorf("failed to cast tx to Payment, data:%+v", tx)
	}
	coreumRecipient := xrpl.DecodeCoreumRecipientFromMemo(paymentTx.Memos)
	if coreumRecipient == nil {
		return XRPLToCoreumTracingInfo{}, errors.New("XRPL tx memo does not include expected structure")
	}

	contractCfg, err := b.contractClient.GetContractConfig(ctx)
	if err != nil {
		return XRPLToCoreumTracingInfo{}, err
	}

	if paymentTx.Destination.String() != contractCfg.BridgeXRPLAddress {
		return XRPLToCoreumTracingInfo{}, errors.New("the destination is not bridge XRPL address")
	}

	coreumTracingInfo, err := b.contractClient.GetXRPLToCoreumTracingInfo(ctx, xrplTxHash)
	if err != nil {
		return XRPLToCoreumTracingInfo{}, err
	}

	return XRPLToCoreumTracingInfo{
		XRPLTx:        tx.TransactionWithMetaData,
		CoreumTx:      coreumTracingInfo.CoreumTx,
		EvidenceToTxs: coreumTracingInfo.EvidenceToTxs,
	}, nil
}

// GetCoreumToXRPLTracingInfo returns Coreum to XRPL tracing info.
func (b *BridgeClient) GetCoreumToXRPLTracingInfo(
	ctx context.Context,
	coreumTxHash string,
) (CoreumToXRPLTracingInfo, error) {
	b.log.Info(ctx, "Getting Coreum to XRPL transfer tracing info")

	tracingInfo, err := b.contractClient.GetCoreumToXRPLTracingInfo(ctx, coreumTxHash)
	if err != nil {
		return CoreumToXRPLTracingInfo{}, err
	}
	if err != nil {
		return CoreumToXRPLTracingInfo{}, err
	}

	coreumToXRPLTracingInfo := CoreumToXRPLTracingInfo{
		CoreumTx:      tracingInfo.CoreumTx,
		XRPLTx:        nil,
		EvidenceToTxs: tracingInfo.EvidenceToTxs,
	}

	if tracingInfo.XRPLTxHash != nil {
		xrplHash, err := rippledata.NewHash256(*tracingInfo.XRPLTxHash)
		if err != nil {
			return CoreumToXRPLTracingInfo{}, errors.Wrapf(err, "invalid XRPL tx hash:%s", *tracingInfo.XRPLTxHash)
		}
		tx, err := b.xrplRPCClient.Tx(ctx, *xrplHash)
		if err != nil {
			return CoreumToXRPLTracingInfo{}, err
		}
		if tx.GetType() != rippledata.PAYMENT.String() {
			return CoreumToXRPLTracingInfo{}, errors.Errorf(
				"invalid XRPL transaction type, expected %s, got: %s", rippledata.PAYMENT.String(), tx.GetType(),
			)
		}
		coreumToXRPLTracingInfo.XRPLTx = &tx.TransactionWithMetaData
	}

	return coreumToXRPLTracingInfo, nil
}

func (b *BridgeClient) validateXRPLBridgeAccountBalance(
	ctx context.Context,
	xrplBridgeAccount rippledata.Account,
) error {
	requiredXRPLBalance := ComputeXRPLBridgeAccountBalance()
	b.log.Info(
		ctx,
		"Compute required XRPL bridge account balance to init the account",
		zap.Float64("requiredBalance", requiredXRPLBalance),
	)
	xrplBridgeAccountInfo, err := b.xrplRPCClient.AccountInfo(ctx, xrplBridgeAccount)
	if err != nil {
		return err
	}
	xrplBridgeAccountBalance := xrplBridgeAccountInfo.AccountData.Balance
	b.log.Info(
		ctx,
		"Got XRPL bridge account balance",
		zap.Float64("balance", xrplBridgeAccountBalance.Float()),
	)
	if xrplBridgeAccountBalance.Float() < requiredXRPLBalance {
		return errors.Errorf(
			"insufficient XRPL bridge account balance, required:%f, current:%f",
			requiredXRPLBalance, xrplBridgeAccountBalance.Float(),
		)
	}

	return nil
}

func (b *BridgeClient) setUpXRPLBridgeAccount(
	ctx context.Context,
	bridgeAccountKeyName string,
	cfg BootstrappingConfig,
	xrplSignerEntries []rippledata.SignerEntry,
) error {
	xrplBridgeAccount, err := b.xrplTxSigner.Account(bridgeAccountKeyName)
	if err != nil {
		return err
	}

	b.log.Info(ctx, "Enabling rippling")
	enableRipplingTx := rippledata.AccountSet{
		SetFlag: lo.ToPtr(uint32(rippledata.TxDefaultRipple)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.ACCOUNT_SET,
		},
	}
	if _, err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &enableRipplingTx, bridgeAccountKeyName); err != nil {
		return err
	}

	b.log.Info(ctx, "Setting signers rippling")
	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum:  cfg.EvidenceThreshold,
		SignerEntries: xrplSignerEntries,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	if _, err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &signerListSetTx, bridgeAccountKeyName); err != nil {
		return err
	}

	b.log.Info(ctx, "Disabling master key")
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         xrplBridgeAccount,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(rippledata.TxSetDisableMaster)),
	}
	if _, err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &disableMasterKeyTx, bridgeAccountKeyName); err != nil {
		return err
	}

	return nil
}

// ComputeXRPLBridgeAccountBalance computes the min balance required by the XRPL bridge account.
func ComputeXRPLBridgeAccountBalance() float64 {
	return minBalanceToCoverFeeAndTrustLines +
		xrpl.ReserveToActivateAccount +
		// one additional item reserve is needed for a signer list object for multisig.
		float64(xrpl.MaxTicketsToAllocate+1)*xrpl.ReservePerItem
}

// InitBootstrappingConfig creates default bootstrapping config yaml file.
func InitBootstrappingConfig(filePath string) error {
	return saveConfigToFile(filePath, DefaultBootstrappingConfig())
}

// ReadBootstrappingConfig reads config bootstrapping yaml file.
func ReadBootstrappingConfig(filePath string) (BootstrappingConfig, error) {
	fileBytes, err := readConfigFromFile(filePath)
	if err != nil {
		return BootstrappingConfig{}, err
	}

	var config BootstrappingConfig
	if err := yaml.Unmarshal(fileBytes, &config); err != nil {
		return BootstrappingConfig{}, errors.Wrapf(err, "failed to unmarshal file to yaml, path:%s", filePath)
	}

	return config, nil
}

// InitKeysRotationConfig creates empty keys rotation config yaml file.
func InitKeysRotationConfig(filePath string) error {
	return saveConfigToFile(filePath, DefaultKeysRotationConfig())
}

// ReadKeysRotationConfig reads keys rotation config yaml file.
func ReadKeysRotationConfig(filePath string) (KeysRotationConfig, error) {
	fileBytes, err := readConfigFromFile(filePath)
	if err != nil {
		return KeysRotationConfig{}, err
	}

	var config KeysRotationConfig
	if err := yaml.Unmarshal(fileBytes, &config); err != nil {
		return KeysRotationConfig{}, errors.Wrapf(err, "failed to unmarshal file to yaml, path:%s", filePath)
	}

	return config, nil
}

func saveConfigToFile(filePath string, srt any) error {
	dirPath := filepath.Dir(filePath)
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		return errors.Errorf("failed to create dirs by path:%s", dirPath)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrapf(err, "failed to create file, path:%s", filePath)
	}
	defer file.Close()
	yamlStringConfig, err := yaml.Marshal(srt)
	if err != nil {
		return errors.Wrap(err, "failed convert default config to yaml")
	}
	if _, err := file.Write(yamlStringConfig); err != nil {
		return errors.Wrapf(err, "failed to write yaml config file, path:%s", filePath)
	}

	return nil
}

func readConfigFromFile(filePath string) ([]byte, error) {
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0o600)
	defer file.Close() //nolint:staticcheck //we accept the error ignoring
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.Errorf("config file does not exist, path:%s", filePath)
	}
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read bytes from file does not exist, path:%s", filePath)
	}

	return fileBytes, nil
}

func (b *BridgeClient) autoFillSignSubmitAndAwaitXRPLTx(
	ctx context.Context,
	tx rippledata.Transaction,
	signerKeyName string,
) (string, error) {
	sender, err := b.xrplTxSigner.Account(signerKeyName)
	if err != nil {
		return "", err
	}
	if err := b.xrplRPCClient.AutoFillTx(ctx, tx, sender, xrpl.MaxAllowedXRPLSigners); err != nil {
		return "", err
	}
	if err := b.xrplTxSigner.Sign(tx, signerKeyName); err != nil {
		return "", err
	}

	b.log.Info(ctx, "Submitting XRPL transaction", zap.String("txHash", tx.GetHash().String()))
	if err = b.xrplRPCClient.SubmitAndAwaitSuccess(ctx, tx); err != nil {
		return "", err
	}
	b.log.Info(ctx, "Successfully submitted transaction", zap.String("txHash", tx.GetHash().String()))

	return tx.GetHash().String(), nil
}
