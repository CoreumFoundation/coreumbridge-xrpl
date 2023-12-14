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
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreum/v3/pkg/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	ticketsToAllocate    = 250
	minBalanceToCoverFee = float64(1)
)

// ContractClient is the interface for the contract client.
type ContractClient interface {
	DeployAndInstantiate(
		ctx context.Context,
		sender sdk.AccAddress,
		byteCode []byte,
		config coreum.InstantiationConfig,
	) (sdk.AccAddress, error)
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
}

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	AccountInfo(ctx context.Context, acc rippledata.Account) (xrpl.AccountInfoResult, error)
	AutoFillTx(ctx context.Context, tx rippledata.Transaction, sender rippledata.Account) error
	Submit(ctx context.Context, tx rippledata.Transaction) (xrpl.SubmitResult, error)
	SubmitAndAwaitSuccess(ctx context.Context, tx rippledata.Transaction) error
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
	log            logger.Logger
	clientCtx      client.Context
	contractClient ContractClient
	xrplRPCClient  XRPLRPCClient
	xrplTxSigner   XRPLTxSigner
}

// NewBridgeClient returns a new instance of the BridgeClient.
func NewBridgeClient(
	log logger.Logger,
	clientCtx client.Context,
	contractClient ContractClient,
	xrplRPCClient XRPLRPCClient,
	xrplTxSigner XRPLTxSigner,
) *BridgeClient {
	return &BridgeClient{
		log:            log,
		clientCtx:      clientCtx,
		contractClient: contractClient,
		xrplRPCClient:  xrplRPCClient,
		xrplTxSigner:   xrplTxSigner,
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
		logger.StringField("keyName", bridgeAccountKeyName),
		logger.StringField("xrplAddress", xrplBridgeAccount.String()),
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
	b.log.Info(ctx, "Deploying contract", logger.AnyField("settings", instantiationCfg))
	contractAddress, err := b.contractClient.DeployAndInstantiate(ctx, senderAddress, contactByteCode, instantiationCfg)
	b.log.Info(ctx, "Contract is deployed successfully", logger.StringField("address", contractAddress.String()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy contract")
	}

	if err := b.setUpXRPLBridgeAccount(ctx, bridgeAccountKeyName, cfg, xrplSignerEntries); err != nil {
		return nil, err
	}

	b.log.Info(ctx, "The XRPL bridge account is ready", logger.StringField("address", xrplBridgeAccount.String()))
	return contractAddress, nil
}

func (b *BridgeClient) buildRelayersFromBootstrappingConfig(
	ctx context.Context,
	cfg BootstrappingConfig,
) ([]coreum.Relayer, []rippledata.SignerEntry, error) {
	coreumAuthClient := authtypes.NewQueryClient(b.clientCtx)
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
		logger.Float64Field("requiredBalance", requiredXRPLBalance),
	)
	xrplBridgeAccountInfo, err := b.xrplRPCClient.AccountInfo(ctx, xrplBridgeAccount)
	if err != nil {
		return err
	}
	xrplBridgeAccountBalance := xrplBridgeAccountInfo.AccountData.Balance
	b.log.Info(
		ctx,
		"Got XRPL bridge account balance",
		logger.Float64Field("balance", xrplBridgeAccountBalance.Float()),
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
	return minBalanceToCoverFee +
		xrpl.ReserveToActivateAccount +
		ticketsToAllocate*xrpl.ReservePerTicket +
		float64(signersCount)*xrpl.ReservePerSigner
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

	return b.xrplRPCClient.SubmitAndAwaitSuccess(ctx, tx)
}