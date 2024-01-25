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
	ClaimRefund(
		ctx context.Context,
		sender sdk.AccAddress,
		pendingRefundID string,
	) (*sdk.TxResponse, error)
	GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error)
	ClaimFees(
		ctx context.Context,
		sender sdk.AccAddress,
		amounts []sdk.Coin,
	) (*sdk.TxResponse, error)
}

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	AccountInfo(ctx context.Context, acc rippledata.Account) (xrpl.AccountInfoResult, error)
	AutoFillTx(ctx context.Context, tx rippledata.Transaction, sender rippledata.Account) error
	Submit(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error)
	SubmitAndAwaitSuccess(ctx context.Context, tx rippledata.Transaction) error
	AccountLines(
		ctx context.Context,
		account rippledata.Account,
		ledgerIndex any,
		marker string,
	) (xrpl.AccountLinesResult, error)
}

// XRPLTxSigner is XRPL transaction signer.
type XRPLTxSigner interface {
	Account(keyName string) (rippledata.Account, error)
	Sign(tx rippledata.Transaction, keyName string) error
}

// RelayerBootstrappingConfig is relayer config used for the bootstrapping.
type RelayerBootstrappingConfig struct {
	CoreumAddress string `yaml:"coreum_address"`
	XRPLAddress   string `yaml:"xrpl_address"`
	XRPLPubKey    string `yaml:"xrpl_pub_key"`
}

// BootstrappingConfig the struct contains the setting for the bridge XRPL account creation and contract deployment.
type BootstrappingConfig struct {
	Owner                       string                       `yaml:"owner"`
	Admin                       string                       `yaml:"admin"`
	Relayers                    []RelayerBootstrappingConfig `yaml:"relayers"`
	EvidenceThreshold           int                          `yaml:"evidence_threshold"`
	UsedTicketSequenceThreshold int                          `yaml:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         string                       `yaml:"trust_set_limit_amount"`
	ContractByteCodePath        string                       `yaml:"contract_bytecode_path"`
	SkipXRPLBalanceValidation   bool                         `yaml:"-"`
}

// DefaultBootstrappingConfig returns default BootstrappingConfig.
func DefaultBootstrappingConfig() BootstrappingConfig {
	return BootstrappingConfig{
		Owner:                       "",
		Admin:                       "",
		Relayers:                    []RelayerBootstrappingConfig{{}},
		EvidenceThreshold:           0,
		UsedTicketSequenceThreshold: 150,
		TrustSetLimitAmount:         sdkmath.NewIntWithDecimal(1, 35).String(),
		ContractByteCodePath:        "",
		SkipXRPLBalanceValidation:   false,
	}
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

// Bootstrap creates initial XRPL bridge multi-signing account the disabled master key,
// enabled rippling on it deploys the bridge contract with the provided settings.
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
		if err = b.validateXRPLBridgeAccountBalance(ctx, len(cfg.Relayers), xrplBridgeAccount); err != nil {
			return nil, err
		}
	}
	// validate the config and fill required objects
	relayers, xrplSignerEntries, err := b.buildRelayersFromBootstrappingConfig(ctx, cfg)
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
	}
	b.log.Info(ctx, "Deploying contract", zap.Any("settings", instantiationCfg))
	contractAddress, err := b.contractClient.DeployAndInstantiate(ctx, senderAddress, contactByteCode, instantiationCfg)
	b.log.Info(ctx, "Contract is deployed successfully", zap.String("address", contractAddress.String()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy contract")
	}

	if err := b.setUpXRPLBridgeAccount(ctx, bridgeAccountKeyName, cfg, xrplSignerEntries); err != nil {
		return nil, err
	}

	b.log.Info(ctx, "The XRPL bridge account is ready", zap.String("address", xrplBridgeAccount.String()))
	return contractAddress, nil
}

// GetContractConfig returns contract config.
func (b *BridgeClient) GetContractConfig(ctx context.Context) (coreum.ContractConfig, error) {
	return b.contractClient.GetContractConfig(ctx)
}

// RecoverTickets recovers tickets allocation.
func (b *BridgeClient) RecoverTickets(
	ctx context.Context,
	ownerAddress sdk.AccAddress,
	ticketsToAllocate uint32,
) error {
	b.log.Info(ctx, "Recovering tickets", zap.Uint32("numberOfTickets", xrpl.MaxTicketsToAllocate))
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
		lo.ToPtr(ticketsToAllocate),
	)
	if err != nil {
		return err
	}

	b.log.Info(
		ctx,
		"Successfully submitted recovery tickets transaction",
		zap.Uint32("numberOfTickets", xrpl.MaxTicketsToAllocate),
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

// SendFromCoreumToXRPL sends tokens form Coreum to XRPL.
func (b *BridgeClient) SendFromCoreumToXRPL(
	ctx context.Context,
	sender sdk.AccAddress,
	recipient rippledata.Account,
	amount sdk.Coin,
	deliverAmount *sdkmath.Int,
) error {
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
		return err
	}

	b.log.Info(
		ctx,
		"Successfully sent tx to send from Coreum to XRPL",
		zap.String("txHash", txRes.TxHash),
	)

	return nil
}

// SendFromXRPLToCoreum sends tokens form XRPL to Coreum.
func (b *BridgeClient) SendFromXRPLToCoreum(
	ctx context.Context,
	senderKeyName string,
	amount rippledata.Amount,
	recipient sdk.AccAddress,
) error {
	senderAccount, err := b.xrplTxSigner.Account(senderKeyName)
	if err != nil {
		return err
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
		return err
	}
	xrplBridgeAddress, err := rippledata.NewAccountFromAddress(cfg.BridgeXRPLAddress)
	if err != nil {
		return errors.Wrapf(
			err,
			"failed to convert BridgeXRPLAddress from contract to rippledata.Account, address:%s",
			cfg.BridgeXRPLAddress,
		)
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(recipient)
	if err != nil {
		return err
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
		},
	}

	return b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &trustSetTx, senderKeyName)
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

	b.log.Info(
		ctx,
		"Successfully sent tx to update XRPL token",
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
	balances := make([]rippledata.Amount, 0)
	accInfo, err := b.xrplRPCClient.AccountInfo(ctx, acc)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get XRPL account info, address:%s", acc.String())
	}
	balances = append(balances, rippledata.Amount{
		Value:    accInfo.AccountData.Balance,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	})

	marker := ""
	for {
		accLines, err := b.xrplRPCClient.AccountLines(ctx, acc, "closed", marker)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get XRPL account lines, address:%s", acc.String())
		}
		for _, line := range accLines.Lines {
			lineCopy := line
			balances = append(balances, rippledata.Amount{
				Value:    &lineCopy.Balance.Value,
				Currency: lineCopy.Currency,
				Issuer:   lineCopy.Account,
			})
		}
		if accLines.Marker == "" {
			break
		}
		marker = accLines.Marker
	}

	return balances, nil
}

// ClaimRefund claims transaction/operation refund.
func (b *BridgeClient) ClaimRefund(
	ctx context.Context,
	sender sdk.AccAddress,
	pendingRefundID string,
) (*sdk.TxResponse, error) {
	return b.contractClient.ClaimRefund(ctx, sender, pendingRefundID)
}

// GetFeesCollected returns the fees collected for the address.
func (b *BridgeClient) GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error) {
	return b.contractClient.GetFeesCollected(ctx, address)
}

func (b *BridgeClient) buildRelayersFromBootstrappingConfig(
	ctx context.Context,
	cfg BootstrappingConfig,
) ([]coreum.Relayer, []rippledata.SignerEntry, error) {
	coreumAuthClient := authtypes.NewQueryClient(b.coreumClientCtx)
	relayers := make([]coreum.Relayer, 0, len(cfg.Relayers))
	xrplSignerEntries := make([]rippledata.SignerEntry, 0)
	for _, relayer := range cfg.Relayers {
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
		relayers = append(relayers, coreum.Relayer{
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

	return relayers, xrplSignerEntries, nil
}

func (b *BridgeClient) validateXRPLBridgeAccountBalance(
	ctx context.Context,
	relayersCount int,
	xrplBridgeAccount rippledata.Account,
) error {
	requiredXRPLBalance := ComputeXRPLBrideAccountBalance(relayersCount)
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
	if err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &enableRipplingTx, bridgeAccountKeyName); err != nil {
		return err
	}

	b.log.Info(ctx, "Setting signers rippling")
	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum:  uint32(cfg.EvidenceThreshold),
		SignerEntries: xrplSignerEntries,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	if err := b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &signerListSetTx, bridgeAccountKeyName); err != nil {
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
	return b.autoFillSignSubmitAndAwaitXRPLTx(ctx, &disableMasterKeyTx, bridgeAccountKeyName)
}

// ComputeXRPLBrideAccountBalance computes the min balance required by the XRPL bridge account.
func ComputeXRPLBrideAccountBalance(signersCount int) float64 {
	return minBalanceToCoverFeeAndTrustLines +
		xrpl.ReserveToActivateAccount +
		float64(xrpl.MaxTicketsToAllocate)*xrpl.ReservePerItem +
		float64(signersCount)*xrpl.ReservePerItem
}

// InitBootstrappingConfig creates default bootstrapping config yaml file.
func InitBootstrappingConfig(filePath string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return errors.Errorf("failed to create dirs by path:%s", filePath)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrapf(err, "failed to create config file, path:%s", filePath)
	}
	defer file.Close()
	yamlStringConfig, err := yaml.Marshal(DefaultBootstrappingConfig())
	if err != nil {
		return errors.Wrap(err, "failed convert default config to yaml")
	}
	if _, err := file.Write(yamlStringConfig); err != nil {
		return errors.Wrapf(err, "failed to write yaml config file, path:%s", filePath)
	}

	return nil
}

// ReadBootstrappingConfig reads config yaml file.
func ReadBootstrappingConfig(filePath string) (BootstrappingConfig, error) {
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0o600)
	defer file.Close() //nolint:staticcheck //we accept the error ignoring
	if errors.Is(err, os.ErrNotExist) {
		return BootstrappingConfig{}, errors.Errorf("config file does not exist, path:%s", filePath)
	}
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return BootstrappingConfig{}, errors.Wrapf(err, "failed to read bytes from file does not exist, path:%s", filePath)
	}

	var config BootstrappingConfig
	if err := yaml.Unmarshal(fileBytes, &config); err != nil {
		return BootstrappingConfig{}, errors.Wrapf(err, "failed to unmarshal file to yaml, path:%s", filePath)
	}

	return config, nil
}

func (b *BridgeClient) autoFillSignSubmitAndAwaitXRPLTx(
	ctx context.Context,
	tx rippledata.Transaction,
	signerKeyName string,
) error {
	sender, err := b.xrplTxSigner.Account(signerKeyName)
	if err != nil {
		return err
	}
	if err := b.xrplRPCClient.AutoFillTx(ctx, tx, sender); err != nil {
		return err
	}
	if err := b.xrplTxSigner.Sign(tx, signerKeyName); err != nil {
		return err
	}

	b.log.Info(
		ctx,
		"Submitting XRPL transaction",
		zap.String("txHash", tx.GetHash().String()),
		zap.Any("tx", tx),
	)
	if err = b.xrplRPCClient.SubmitAndAwaitSuccess(ctx, tx); err != nil {
		return err
	}
	b.log.Info(ctx, "Successfully submitted transaction", zap.String("txHash", tx.GetHash().String()))

	return nil
}
