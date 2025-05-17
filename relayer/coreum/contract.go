//nolint:tagliatelle // contract spec
package coreum

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	sdktxtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreum/v5/pkg/client"
	"github.com/CoreumFoundation/coreum/v5/testutil/event"
	assetfttypes "github.com/CoreumFoundation/coreum/v5/x/asset/ft/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

const (
	contractLabel = "coreumbridge-xrpl"
	// RelayerCoreumMemoPrefix is memo prefix for the relayer transaction.
	RelayerCoreumMemoPrefix = "Coreum XRPL bridge relayer version:"

	eventAttributeAction           = "action"
	eventAttributeHash             = "hash"
	eventAttributeThresholdReached = "threshold_reached"
	eventAttributeOperationID      = "operation_id"
	eventValueSaveAction           = "save_evidence"
)

// ExecMethod is contract exec method.
type ExecMethod string

// ExecMethods.
const (
	ExecMethodUpdateOwnership         ExecMethod = "update_ownership"
	ExecMethodRegisterCoreumToken     ExecMethod = "register_coreum_token"
	ExecMethodRegisterXRPLToken       ExecMethod = "register_xrpl_token"
	ExecMethodSaveEvidence            ExecMethod = "save_evidence"
	ExecMethodRecoverTickets          ExecMethod = "recover_tickets"
	ExecMethodSaveSignature           ExecMethod = "save_signature"
	ExecSendToXRPL                    ExecMethod = "send_to_xrpl"
	ExecRecoveryXRPLTokenRegistration ExecMethod = "recover_xrpl_token_registration"
	ExecClaimRelayersFees             ExecMethod = "claim_relayer_fees"
	ExecUpdateXRPLToken               ExecMethod = "update_xrpl_token"
	ExecUpdateCoreumToken             ExecMethod = "update_coreum_token"
	ExecClaimRefund                   ExecMethod = "claim_refund"
	ExecRotateKeys                    ExecMethod = "rotate_keys"
	ExecHaltBridge                    ExecMethod = "halt_bridge"
	ExecResumeBridge                  ExecMethod = "resume_bridge"
	ExecUpdateXRPLBaseFee             ExecMethod = "update_xrpl_base_fee"
	ExecUpdateProhibitedXRPLAddresses ExecMethod = "update_prohibited_xrpl_addresses"
	ExecCancelPendingOperation        ExecMethod = "cancel_pending_operation"
)

// TransactionResult is transaction result.
type TransactionResult string

// TransactionResult values.
const (
	TransactionResultAccepted TransactionResult = "accepted"
	TransactionResultRejected TransactionResult = "rejected"
	TransactionResultInvalid  TransactionResult = "invalid"
)

// TokenState is token state.
type TokenState string

// TokenState values.
const (
	TokenStateEnabled    TokenState = "enabled"
	TokenStateDisabled   TokenState = "disabled"
	TokenStateProcessing TokenState = "processing"
	TokenStateInactive   TokenState = "inactive"
)

// BridgeState is bridge state.
type BridgeState string

// BridgeState values.
const (
	BridgeStateActive BridgeState = "active"
	BridgeStateHalted BridgeState = "halted"
)

// QueryMethod is contract query method.
type QueryMethod string

// QueryMethods.
const (
	QueryMethodConfig                  QueryMethod = "config"
	QueryMethodOwnership               QueryMethod = "ownership"
	QueryMethodXRPLTokens              QueryMethod = "xrpl_tokens"
	QueryMethodFeesCollected           QueryMethod = "fees_collected"
	QueryMethodCoreumTokens            QueryMethod = "coreum_tokens"
	QueryMethodPendingOperations       QueryMethod = "pending_operations"
	QueryMethodAvailableTickets        QueryMethod = "available_tickets"
	QueryMethodPendingRefunds          QueryMethod = "pending_refunds"
	QueryMethodTransactionEvidences    QueryMethod = "transaction_evidences"
	QueryMethodProhibitedXRPLAddresses QueryMethod = "prohibited_xrpl_addresses"
)

// Relayer is the relayer information in the contract config.
type Relayer struct {
	CoreumAddress sdk.AccAddress `json:"coreum_address"`
	XRPLAddress   string         `json:"xrpl_address"`
	XRPLPubKey    string         `json:"xrpl_pub_key"`
}

// InstantiationConfig holds attributes used for the contract instantiation.
type InstantiationConfig struct {
	Owner                       sdk.AccAddress
	Admin                       sdk.AccAddress
	Relayers                    []Relayer
	EvidenceThreshold           uint32
	UsedTicketSequenceThreshold uint32
	TrustSetLimitAmount         sdkmath.Int
	BridgeXRPLAddress           string
	XRPLBaseFee                 uint32
}

// ContractConfig is contract config.
type ContractConfig struct {
	Relayers                    []Relayer   `json:"relayers"`
	EvidenceThreshold           uint32      `json:"evidence_threshold"`
	UsedTicketSequenceThreshold uint32      `json:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         sdkmath.Int `json:"trust_set_limit_amount"`
	BridgeXRPLAddress           string      `json:"bridge_xrpl_address"`
	BridgeState                 BridgeState `json:"bridge_state"`
	XRPLBaseFee                 uint32      `json:"xrpl_base_fee"`
}

// ContractOwnership is owner contract config.
type ContractOwnership struct {
	Owner        sdk.AccAddress `json:"owner"`
	PendingOwner sdk.AccAddress `json:"pending_owner"`
}

// XRPLToken is XRPL token representation on coreum.
type XRPLToken struct {
	Issuer           string      `json:"issuer"`
	Currency         string      `json:"currency"`
	CoreumDenom      string      `json:"coreum_denom"`
	SendingPrecision int32       `json:"sending_precision"`
	MaxHoldingAmount sdkmath.Int `json:"max_holding_amount"`
	State            TokenState  `json:"state"`
	BridgingFee      sdkmath.Int `json:"bridging_fee"`
}

// CoreumToken is coreum token registered on the contract.
//
//nolint:revive //kept for the better naming convention.
type CoreumToken struct {
	Denom            string      `json:"denom"`
	Decimals         uint32      `json:"decimals"`
	XRPLCurrency     string      `json:"xrpl_currency"`
	SendingPrecision int32       `json:"sending_precision"`
	MaxHoldingAmount sdkmath.Int `json:"max_holding_amount"`
	State            TokenState  `json:"state"`
	BridgingFee      sdkmath.Int `json:"bridging_fee"`
}

// XRPLToCoreumTransferEvidence is evidence with values represented sending from XRPL to coreum.
type XRPLToCoreumTransferEvidence struct {
	TxHash    string         `json:"tx_hash"`
	Issuer    string         `json:"issuer"`
	Currency  string         `json:"currency"`
	Amount    sdkmath.Int    `json:"amount"`
	Recipient sdk.AccAddress `json:"recipient"`
}

// XRPLTransactionResultEvidence is type which contains common transaction result data.
type XRPLTransactionResultEvidence struct {
	TxHash            string            `json:"tx_hash,omitempty"`
	AccountSequence   *uint32           `json:"account_sequence"`
	TicketSequence    *uint32           `json:"ticket_sequence"`
	TransactionResult TransactionResult `json:"transaction_result"`
}

// XRPLTransactionResultTicketsAllocationEvidence is evidence of the tickets allocation transaction.
type XRPLTransactionResultTicketsAllocationEvidence struct {
	XRPLTransactionResultEvidence
	// we don't use the tag here since we don't use that struct as transport object
	Tickets []uint32
}

// XRPLTransactionResultTrustSetEvidence is evidence of the trust set transaction.
type XRPLTransactionResultTrustSetEvidence struct {
	XRPLTransactionResultEvidence
}

// XRPLTransactionResultCoreumToXRPLTransferEvidence is evidence of the sending from XRPL to coreum.
type XRPLTransactionResultCoreumToXRPLTransferEvidence struct {
	XRPLTransactionResultEvidence
}

// XRPLTransactionResultKeysRotationEvidence is evidence of the multi-signing account keys rotation.
type XRPLTransactionResultKeysRotationEvidence struct {
	XRPLTransactionResultEvidence
}

// Signature is a pair of the relayer provided the signature and signature string.
type Signature struct {
	RelayerCoreumAddress sdk.AccAddress `json:"relayer_coreum_address"`
	Signature            string         `json:"signature"`
}

// OperationTypeAllocateTickets is allocated tickets operation type.
type OperationTypeAllocateTickets struct {
	Number uint32 `json:"number"`
}

// OperationTypeTrustSet is trust set operation type.
type OperationTypeTrustSet struct {
	Issuer              string      `json:"issuer"`
	Currency            string      `json:"currency"`
	TrustSetLimitAmount sdkmath.Int `json:"trust_set_limit_amount"`
}

// OperationTypeCoreumToXRPLTransfer is coreum to XRPL transfer operation type.
type OperationTypeCoreumToXRPLTransfer struct {
	Issuer    string       `json:"issuer"`
	Currency  string       `json:"currency"`
	Amount    sdkmath.Int  `json:"amount"`
	MaxAmount *sdkmath.Int `json:"max_amount,omitempty"`
	Recipient string       `json:"recipient"`
}

// OperationTypeRotateKeys is XRPL multi-signing address keys rotation operation type.
type OperationTypeRotateKeys struct {
	NewRelayers          []Relayer `json:"new_relayers"`
	NewEvidenceThreshold int       `json:"new_evidence_threshold"`
}

// OperationType is operation type.
type OperationType struct {
	AllocateTickets      *OperationTypeAllocateTickets      `json:"allocate_tickets,omitempty"`
	TrustSet             *OperationTypeTrustSet             `json:"trust_set,omitempty"`
	CoreumToXRPLTransfer *OperationTypeCoreumToXRPLTransfer `json:"coreum_to_xrpl_transfer,omitempty"`
	RotateKeys           *OperationTypeRotateKeys           `json:"rotate_keys,omitempty"`
}

// Operation is contract operation which should be signed and executed.
type Operation struct {
	Version         uint32        `json:"version"`
	TicketSequence  uint32        `json:"ticket_sequence"`
	AccountSequence uint32        `json:"account_sequence"`
	Signatures      []Signature   `json:"signatures"`
	OperationType   OperationType `json:"operation_type"`
	XRPLBaseFee     uint32        `json:"xrpl_base_fee"`
}

// GetOperationID returns operation ID.
func (o Operation) GetOperationID() uint32 {
	if o.TicketSequence != 0 {
		return o.TicketSequence
	}

	return o.AccountSequence
}

// SendToXRPLRequest defines single request to send from coreum to XRPL.
type SendToXRPLRequest struct {
	Recipient     string       `json:"recipient"`
	DeliverAmount *sdkmath.Int `json:"deliver_amount,omitempty"`
	Amount        sdk.Coin     `json:"-"`
}

// SaveSignatureRequest defines single request to save relayer signature.
type SaveSignatureRequest struct {
	OperationID      uint32
	OperationVersion uint32
	Signature        string
}

// PendingRefund holds the pending refund information.
type PendingRefund struct {
	ID         string   `json:"id"`
	Coin       sdk.Coin `json:"coin"`
	XRPLTxHash string   `json:"xrpl_tx_hash"`
}

// TransactionEvidence is the transaction evidence.
type TransactionEvidence struct {
	Hash             string           `json:"hash"`
	RelayerAddresses []sdk.AccAddress `json:"relayer_addresses"`
}

// DataToTx is data to tx mapping.
type DataToTx[T any] struct {
	Evidence T
	Tx       *sdk.TxResponse
}

// XRPLToCoreumTracingInfo is XRPL to Coreum tracing info.
type XRPLToCoreumTracingInfo struct {
	CoreumTx      *sdk.TxResponse
	EvidenceToTxs []DataToTx[XRPLToCoreumTransferEvidence]
}

// CoreumToXRPLTracingInfo is Coreum to XRPL tracing info.
//
//nolint:revive //kept for the better naming convention.
type CoreumToXRPLTracingInfo struct {
	XRPLTxHashes  []string
	CoreumTx      sdk.TxResponse
	EvidenceToTxs [][]DataToTx[XRPLTransactionResultEvidence]
}

// SaveEvidenceRequest is save_evidence method request.
type SaveEvidenceRequest struct {
	Evidence evidence `json:"evidence"`
}

// ExecutePayload aggregates execute contract payload.
type ExecutePayload struct {
	SaveEvidence *SaveEvidenceRequest `json:"save_evidence,omitempty"`
	SendToXRPL   *SendToXRPLRequest   `json:"send_to_xrpl,omitempty"`
}

// ******************** Internal transport object  ********************

type instantiateRequest struct {
	Owner                       sdk.AccAddress `json:"owner"`
	Relayers                    []Relayer      `json:"relayers"`
	EvidenceThreshold           uint32         `json:"evidence_threshold"`
	UsedTicketSequenceThreshold uint32         `json:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         sdkmath.Int    `json:"trust_set_limit_amount"`
	BridgeXRPLAddress           string         `json:"bridge_xrpl_address"`
	XRPLBaseFee                 uint32         `json:"xrpl_base_fee"`
}

type transferOwnershipRequest struct {
	TransferOwnership struct {
		NewOwner sdk.AccAddress `json:"new_owner"`
	} `json:"transfer_ownership"`
}

type registerCoreumTokenRequest struct {
	Denom            string      `json:"denom"`
	Decimals         uint32      `json:"decimals"`
	SendingPrecision int32       `json:"sending_precision"`
	MaxHoldingAmount sdkmath.Int `json:"max_holding_amount"`
	BridgingFee      sdkmath.Int `json:"bridging_fee"`
}

type registerXRPLTokenRequest struct {
	Issuer           string      `json:"issuer"`
	Currency         string      `json:"currency"`
	SendingPrecision int32       `json:"sending_precision"`
	MaxHoldingAmount sdkmath.Int `json:"max_holding_amount"`
	BridgingFee      sdkmath.Int `json:"bridging_fee"`
}

type recoverTicketsRequest struct {
	AccountSequence uint32  `json:"account_sequence"`
	NumberOfTickets *uint32 `json:"number_of_tickets,omitempty"`
}

type rotateKeysRequest struct {
	NewRelayers          []Relayer `json:"new_relayers"`
	NewEvidenceThreshold uint32    `json:"new_evidence_threshold"`
}

type saveSignatureRequest struct {
	OperationID      uint32 `json:"operation_id"`
	OperationVersion uint32 `json:"operation_version"`
	Signature        string `json:"signature"`
}

type recoverXRPLTokenRegistrationRequest struct {
	Issuer   string `json:"issuer"`
	Currency string `json:"currency"`
}

type claimFeesRequest struct {
	Amounts []sdk.Coin `json:"amounts"`
}

type updateXRPLTokenRequest struct {
	Issuer           string       `json:"issuer"`
	Currency         string       `json:"currency"`
	State            *TokenState  `json:"state,omitempty"`
	SendingPrecision *int32       `json:"sending_precision,omitempty"`
	MaxHoldingAmount *sdkmath.Int `json:"max_holding_amount,omitempty"`
	BridgingFee      *sdkmath.Int `json:"bridging_fee,omitempty"`
}

type updateCoreumTokenRequest struct {
	Denom            string       `json:"denom"`
	State            *TokenState  `json:"state,omitempty"`
	SendingPrecision *int32       `json:"sending_precision,omitempty"`
	MaxHoldingAmount *sdkmath.Int `json:"max_holding_amount,omitempty"`
	BridgingFee      *sdkmath.Int `json:"bridging_fee,omitempty"`
}

type claimRefundRequest struct {
	PendingRefundID string `json:"pending_refund_id"`
}

type updateXRPLBaseFeeRequest struct {
	XRPLBaseFee uint32 `json:"xrpl_base_fee"`
}

type updateProhibitedXRPLAddressesRequest struct {
	ProhibitedXRPLAddresses []string `json:"prohibited_xrpl_addresses"`
}

type cancelPendingOperationRequest struct {
	OperationID uint32 `json:"operation_id"`
}

type xrplTransactionEvidenceTicketsAllocationOperationResult struct {
	Tickets []uint32 `json:"tickets"`
}

type xrplTransactionEvidenceOperationResult struct {
	TicketsAllocation *xrplTransactionEvidenceTicketsAllocationOperationResult `json:"tickets_allocation,omitempty"`
}

type xrplTransactionResultEvidence struct {
	XRPLTransactionResultEvidence
	OperationResult *xrplTransactionEvidenceOperationResult `json:"operation_result,omitempty"`
}

type evidence struct {
	XRPLToCoreumTransfer  *XRPLToCoreumTransferEvidence  `json:"xrpl_to_coreum_transfer,omitempty"`
	XRPLTransactionResult *xrplTransactionResultEvidence `json:"xrpl_transaction_result,omitempty"`
}

type xrplTokensResponse struct {
	LastKey string      `json:"last_key"`
	Tokens  []XRPLToken `json:"tokens"`
}

type coreumTokensResponse struct {
	LastKey string        `json:"last_key"`
	Tokens  []CoreumToken `json:"tokens"`
}

type pendingOperationsResponse struct {
	LastKey    uint32      `json:"last_key"`
	Operations []Operation `json:"operations"`
}

type availableTicketsResponse struct {
	Tickets []uint32 `json:"tickets"`
}

type feesCollectedResponse struct {
	FeesCollected []sdk.Coin `json:"fees_collected"`
}

type pendingRefundsRequest struct {
	StartAfterKey []string       `json:"start_after_key,omitempty"`
	Limit         *uint32        `json:"limit,omitempty"`
	Address       sdk.AccAddress `json:"address"`
}

type pendingRefundsResponse struct {
	LastKey        []string        `json:"last_key"`
	PendingRefunds []PendingRefund `json:"pending_refunds"`
}

type transactionEvidencesResponse struct {
	LastKey              string                `json:"last_key"`
	TransactionEvidences []TransactionEvidence `json:"transaction_evidences"`
}

type prohibitedXRPLAddressesResponse struct {
	ProhibitedXRPLAddresses []string `json:"prohibited_xrpl_addresses"`
}

type pagingStringKeyRequest struct {
	StartAfterKey string  `json:"start_after_key,omitempty"`
	Limit         *uint32 `json:"limit,omitempty"`
}

type pagingUint32KeyRequest struct {
	StartAfterKey *uint32 `json:"start_after_key,omitempty"`
	Limit         *uint32 `json:"limit,omitempty"`
}

type execRequest struct {
	Body  any
	Funds sdk.Coins
}

// ******************** Client ********************

// ContractClientConfig represent the ContractClient config.
type ContractClientConfig struct {
	ContractAddress       sdk.AccAddress
	GasAdjustment         float64
	GasPriceAdjustment    sdkmath.LegacyDec
	PageLimit             uint32
	OutOfGasRetryDelay    time.Duration
	OutOfGasRetryAttempts uint32
	TxsQueryPageLimit     uint32
}

// DefaultContractClientConfig returns default ContractClient config.
func DefaultContractClientConfig(contractAddress sdk.AccAddress) ContractClientConfig {
	return ContractClientConfig{
		ContractAddress:       contractAddress,
		GasAdjustment:         1.4,
		GasPriceAdjustment:    sdkmath.LegacyMustNewDecFromStr("1.2"),
		PageLimit:             50,
		OutOfGasRetryDelay:    500 * time.Millisecond,
		OutOfGasRetryAttempts: 5,
		TxsQueryPageLimit:     1000,
	}
}

// ContractClient is the bridge contract client.
type ContractClient struct {
	cfg                ContractClientConfig
	log                logger.Logger
	clientCtx          client.Context
	wasmClient         wasmtypes.QueryClient
	assetftClient      assetfttypes.QueryClient
	cometServiceClient sdktxtypes.ServiceClient

	execMu sync.Mutex
}

// NewContractClient returns a new instance of the ContractClient.
func NewContractClient(cfg ContractClientConfig, log logger.Logger, clientCtx client.Context) *ContractClient {
	return &ContractClient{
		cfg: cfg,
		log: log,
		clientCtx: clientCtx.
			WithBroadcastMode(flags.BroadcastSync).
			WithAwaitTx(true).WithGasPriceAdjustment(cfg.GasPriceAdjustment).
			WithGasAdjustment(cfg.GasAdjustment),
		wasmClient:         wasmtypes.NewQueryClient(clientCtx),
		assetftClient:      assetfttypes.NewQueryClient(clientCtx),
		cometServiceClient: sdktxtypes.NewServiceClient(clientCtx),

		execMu: sync.Mutex{},
	}
}

// DeployAndInstantiate instantiates the contract.
func (c *ContractClient) DeployAndInstantiate(
	ctx context.Context,
	sender sdk.AccAddress,
	byteCode []byte,
	config InstantiationConfig,
) (sdk.AccAddress, error) {
	_, codeID, err := c.DeployContract(ctx, sender, byteCode)
	if err != nil {
		return nil, err
	}

	reqPayload, err := json.Marshal(instantiateRequest{
		Owner:                       config.Owner,
		Relayers:                    config.Relayers,
		EvidenceThreshold:           config.EvidenceThreshold,
		UsedTicketSequenceThreshold: config.UsedTicketSequenceThreshold,
		TrustSetLimitAmount:         config.TrustSetLimitAmount,
		BridgeXRPLAddress:           config.BridgeXRPLAddress,
		XRPLBaseFee:                 config.XRPLBaseFee,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal instantiate payload")
	}

	issuerFee, err := c.queryAssetFTIssueFee(ctx)
	if err != nil {
		return nil, err
	}

	msg := &wasmtypes.MsgInstantiateContract{
		Sender: sender.String(),
		Admin:  config.Admin.String(),
		CodeID: codeID,
		Label:  contractLabel,
		Msg:    reqPayload,
		// the instantiation requires fee to cover XRP token issuance
		Funds: sdk.NewCoins(issuerFee),
	}

	c.log.Info(ctx, "Instantiating contract.", zap.Any("msg", msg))
	res, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy bytecode")
	}

	contractAddr, err := event.FindStringEventAttribute(
		res.Events, wasmtypes.EventTypeInstantiate, wasmtypes.AttributeKeyContractAddr,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find contract address in the tx result")
	}

	sdkContractAddr, err := sdk.AccAddressFromBech32(contractAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert contract address to sdk.AccAddress")
	}
	c.log.Info(ctx, "The contract is instantiated.", zap.String("address", sdkContractAddr.String()))

	return sdkContractAddr, nil
}

// DeployContract deploys the contract bytecode.
func (c *ContractClient) DeployContract(
	ctx context.Context,
	sender sdk.AccAddress,
	byteCode []byte,
) (*sdk.TxResponse, uint64, error) {
	msgStoreCode := &wasmtypes.MsgStoreCode{
		Sender:       sender.String(),
		WASMByteCode: byteCode,
	}
	c.log.Info(ctx, "Deploying contract bytecode.")

	txRes, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msgStoreCode)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to deploy wasm bytecode")
	}
	// handle the genereate only case
	if txRes == nil {
		return nil, 0, nil
	}
	codeID, err := event.FindUint64EventAttribute(txRes.Events, wasmtypes.EventTypeStoreCode, wasmtypes.AttributeKeyCodeID)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to find code ID in the tx result")
	}
	c.log.Info(ctx, "The contract bytecode is deployed.", zap.Uint64("codeID", codeID))

	return txRes, codeID, nil
}

// MigrateContract calls the executes the contract migration.
func (c *ContractClient) MigrateContract(
	ctx context.Context,
	sender sdk.AccAddress,
	codeID uint64,
) (*sdk.TxResponse, error) {
	msgMigrate := &wasmtypes.MsgMigrateContract{
		Sender:   sender.String(),
		Contract: c.GetContractAddress().String(),
		CodeID:   codeID,
		Msg:      []byte("{}"),
	}

	txRes, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msgMigrate)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to migrate contract, codeID:%d", codeID)
	}
	c.log.Info(ctx, "Contract migrated successfully")

	return txRes, nil
}

// SetContractAddress sets the client contract address if it was not set before.
func (c *ContractClient) SetContractAddress(contractAddress sdk.AccAddress) error {
	if c.cfg.ContractAddress != nil {
		return errors.New("contract address is already set")
	}

	c.cfg.ContractAddress = contractAddress

	return nil
}

// GetContractAddress returns contract address used by the client.
func (c *ContractClient) GetContractAddress() sdk.AccAddress {
	return c.cfg.ContractAddress
}

// IsInitialized returns true if address used by the client is set.
func (c *ContractClient) IsInitialized() bool {
	return !c.cfg.ContractAddress.Empty()
}

// ******************** Execute ********************

// TransferOwnership executes `update_ownership` method with transfer action.
func (c *ContractClient) TransferOwnership(
	ctx context.Context, sender, newOwner sdk.AccAddress,
) (*sdk.TxResponse, error) {
	req := transferOwnershipRequest{}
	req.TransferOwnership.NewOwner = newOwner

	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]transferOwnershipRequest{
			ExecMethodUpdateOwnership: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// AcceptOwnership executes `update_ownership` method with accept action.
func (c *ContractClient) AcceptOwnership(ctx context.Context, sender sdk.AccAddress) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]string{
			ExecMethodUpdateOwnership: "accept_ownership",
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RegisterCoreumToken executes `register_coreum_token` method.
func (c *ContractClient) RegisterCoreumToken(
	ctx context.Context,
	sender sdk.AccAddress,
	denom string,
	decimals uint32,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]registerCoreumTokenRequest{
			ExecMethodRegisterCoreumToken: {
				Denom:            denom,
				Decimals:         decimals,
				SendingPrecision: sendingPrecision,
				MaxHoldingAmount: maxHoldingAmount,
				BridgingFee:      bridgingFee,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RegisterXRPLToken executes `register_xrpl_token` method.
func (c *ContractClient) RegisterXRPLToken(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) (*sdk.TxResponse, error) {
	fee, err := c.queryAssetFTIssueFee(ctx)
	if err != nil {
		return nil, err
	}

	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]registerXRPLTokenRequest{
			ExecMethodRegisterXRPLToken: {
				Issuer:           issuer,
				Currency:         currency,
				SendingPrecision: sendingPrecision,
				MaxHoldingAmount: maxHoldingAmount,
				BridgingFee:      bridgingFee,
			},
		},
		Funds: sdk.NewCoins(fee),
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendXRPLToCoreumTransferEvidence sends an Evidence of an accepted XRPL to coreum transfer transaction.
func (c *ContractClient) SendXRPLToCoreumTransferEvidence(
	ctx context.Context,
	sender sdk.AccAddress,
	evd XRPLToCoreumTransferEvidence,
) (*sdk.TxResponse, error) {
	req := SaveEvidenceRequest{
		Evidence: evidence{
			XRPLToCoreumTransfer: &evd,
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]SaveEvidenceRequest{
			ExecMethodSaveEvidence: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendXRPLTicketsAllocationTransactionResultEvidence sends an Evidence of an accepted
// or rejected ticket allocation transaction.
func (c *ContractClient) SendXRPLTicketsAllocationTransactionResultEvidence(
	ctx context.Context,
	sender sdk.AccAddress,
	evd XRPLTransactionResultTicketsAllocationEvidence,
) (*sdk.TxResponse, error) {
	req := SaveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
				OperationResult: &xrplTransactionEvidenceOperationResult{
					TicketsAllocation: &xrplTransactionEvidenceTicketsAllocationOperationResult{
						Tickets: evd.Tickets,
					},
				},
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]SaveEvidenceRequest{
			ExecMethodSaveEvidence: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendXRPLTrustSetTransactionResultEvidence sends an Evidence of an accepted or rejected trust set transaction.
func (c *ContractClient) SendXRPLTrustSetTransactionResultEvidence(
	ctx context.Context,
	sender sdk.AccAddress,
	evd XRPLTransactionResultTrustSetEvidence,
) (*sdk.TxResponse, error) {
	req := SaveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]SaveEvidenceRequest{
			ExecMethodSaveEvidence: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendCoreumToXRPLTransferTransactionResultEvidence sends an Evidence of an accepted or
// rejected coreum to XRPL transfer transaction.
func (c *ContractClient) SendCoreumToXRPLTransferTransactionResultEvidence(
	ctx context.Context,
	sender sdk.AccAddress,
	evd XRPLTransactionResultCoreumToXRPLTransferEvidence,
) (*sdk.TxResponse, error) {
	req := SaveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]SaveEvidenceRequest{
			ExecMethodSaveEvidence: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendKeysRotationTransactionResultEvidence sends an Evidence of an accepted or
// rejected coreum to XRPL transfer transaction.
func (c *ContractClient) SendKeysRotationTransactionResultEvidence(
	ctx context.Context,
	sender sdk.AccAddress,
	evd XRPLTransactionResultKeysRotationEvidence,
) (*sdk.TxResponse, error) {
	req := SaveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]SaveEvidenceRequest{
			ExecMethodSaveEvidence: req,
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RecoverTickets executes `recover_tickets` method.
func (c *ContractClient) RecoverTickets(
	ctx context.Context,
	sender sdk.AccAddress,
	accountSequence uint32,
	numberOfTickets *uint32,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]recoverTicketsRequest{
			ExecMethodRecoverTickets: {
				AccountSequence: accountSequence,
				NumberOfTickets: numberOfTickets,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SaveSignature executes `save_signature` method.
func (c *ContractClient) SaveSignature(
	ctx context.Context,
	sender sdk.AccAddress,
	operationID uint32,
	operationVersion uint32,
	signature string,
) (*sdk.TxResponse, error) {
	return c.SaveMultipleSignatures(
		ctx,
		sender,
		SaveSignatureRequest{
			OperationID:      operationID,
			OperationVersion: operationVersion,
			Signature:        signature,
		},
	)
}

// SaveMultipleSignatures executes `save_signature` method for each request.
func (c *ContractClient) SaveMultipleSignatures(
	ctx context.Context,
	sender sdk.AccAddress,
	requests ...SaveSignatureRequest,
) (*sdk.TxResponse, error) {
	execRequests := make([]execRequest, 0, len(requests))
	for _, req := range requests {
		execRequests = append(execRequests, execRequest{
			Body: map[ExecMethod]saveSignatureRequest{
				ExecMethodSaveSignature: {
					OperationID:      req.OperationID,
					OperationVersion: req.OperationVersion,
					Signature:        req.Signature,
				},
			},
		})
	}
	txRes, err := c.execute(ctx, sender, execRequests...)
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// SendToXRPL executes `send_to_xrpl` method.
func (c *ContractClient) SendToXRPL(
	ctx context.Context,
	sender sdk.AccAddress,
	recipient string,
	amount sdk.Coin,
	deliverAmount *sdkmath.Int,
) (*sdk.TxResponse, error) {
	return c.MultiSendToXRPL(ctx, sender, SendToXRPLRequest{
		Recipient:     recipient,
		Amount:        amount,
		DeliverAmount: deliverAmount,
	})
}

// MultiSendToXRPL executes `send_to_xrpl` method for each request.
func (c *ContractClient) MultiSendToXRPL(
	ctx context.Context,
	sender sdk.AccAddress,
	requests ...SendToXRPLRequest,
) (*sdk.TxResponse, error) {
	execRequests := make([]execRequest, 0, len(requests))
	for _, req := range requests {
		execRequests = append(execRequests, execRequest{
			Body: map[ExecMethod]SendToXRPLRequest{
				ExecSendToXRPL: req,
			},
			Funds: sdk.NewCoins(req.Amount),
		})
	}
	txRes, err := c.execute(ctx, sender, execRequests...)
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RecoverXRPLTokenRegistration executes `recover_xrpl_token_registration` method.
func (c *ContractClient) RecoverXRPLTokenRegistration(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]recoverXRPLTokenRegistrationRequest{
			ExecRecoveryXRPLTokenRegistration: {
				Issuer:   issuer,
				Currency: currency,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// ClaimRelayerFees calls the contract to claim the fees for a given relayer.
func (c *ContractClient) ClaimRelayerFees(
	ctx context.Context,
	sender sdk.AccAddress,
	amounts sdk.Coins,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]claimFeesRequest{
			ExecClaimRelayersFees: {
				Amounts: amounts,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// UpdateXRPLToken executes `update_xrpl_token` method.
func (c *ContractClient) UpdateXRPLToken(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
	state *TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateXRPLTokenRequest{
			ExecUpdateXRPLToken: {
				Issuer:           issuer,
				Currency:         currency,
				State:            state,
				SendingPrecision: sendingPrecision,
				MaxHoldingAmount: maxHoldingAmount,
				BridgingFee:      bridgingFee,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// UpdateCoreumToken executes `update_coreum_token` method.
func (c *ContractClient) UpdateCoreumToken(
	ctx context.Context,
	sender sdk.AccAddress,
	denom string,
	state *TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateCoreumTokenRequest{
			ExecUpdateCoreumToken: {
				Denom:            denom,
				State:            state,
				SendingPrecision: sendingPrecision,
				MaxHoldingAmount: maxHoldingAmount,
				BridgingFee:      bridgingFee,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// ClaimRefund executes `claim_refund` method.
func (c *ContractClient) ClaimRefund(
	ctx context.Context,
	sender sdk.AccAddress,
	pendingRefundID string,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]claimRefundRequest{
			ExecClaimRefund: {
				PendingRefundID: pendingRefundID,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RotateKeys executes `rotate_keys` method.
func (c *ContractClient) RotateKeys(
	ctx context.Context,
	sender sdk.AccAddress,
	newRelayers []Relayer,
	newEvidenceThreshold uint32,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]rotateKeysRequest{
			ExecRotateKeys: {
				NewRelayers:          newRelayers,
				NewEvidenceThreshold: newEvidenceThreshold,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// HaltBridge executes `halt_bridge` method.
func (c *ContractClient) HaltBridge(
	ctx context.Context,
	sender sdk.AccAddress,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]struct{}{
			ExecHaltBridge: {},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// ResumeBridge executes `resume_bridge` method.
func (c *ContractClient) ResumeBridge(
	ctx context.Context,
	sender sdk.AccAddress,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]struct{}{
			ExecResumeBridge: {},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// UpdateXRPLBaseFee executes `update_xrpl_base_fee` method.
func (c *ContractClient) UpdateXRPLBaseFee(
	ctx context.Context,
	sender sdk.AccAddress,
	xrplBaseFee uint32,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateXRPLBaseFeeRequest{
			ExecUpdateXRPLBaseFee: {
				XRPLBaseFee: xrplBaseFee,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// CancelPendingOperation executes `cancel_pending_operation` method.
func (c *ContractClient) CancelPendingOperation(
	ctx context.Context,
	sender sdk.AccAddress,
	operationID uint32,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]cancelPendingOperationRequest{
			ExecCancelPendingOperation: {
				OperationID: operationID,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// UpdateProhibitedXRPLAddresses executes `update_prohibited_xrpl_addresses` method.
func (c *ContractClient) UpdateProhibitedXRPLAddresses(
	ctx context.Context,
	sender sdk.AccAddress,
	prohibitedXRPLAddresses []string,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateProhibitedXRPLAddressesRequest{
			ExecUpdateProhibitedXRPLAddresses: {
				ProhibitedXRPLAddresses: prohibitedXRPLAddresses,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// ******************** Query ********************

// GetContractConfig returns contract config.
func (c *ContractClient) GetContractConfig(ctx context.Context) (ContractConfig, error) {
	var response ContractConfig
	err := c.query(ctx, map[QueryMethod]struct{}{
		QueryMethodConfig: {},
	}, &response)
	if err != nil {
		return ContractConfig{}, err
	}

	return response, nil
}

// GetContractOwnership returns contract ownership.
func (c *ContractClient) GetContractOwnership(ctx context.Context) (ContractOwnership, error) {
	var response ContractOwnership
	err := c.query(ctx, map[QueryMethod]struct{}{
		QueryMethodOwnership: {},
	}, &response)
	if err != nil {
		return ContractOwnership{}, err
	}

	return response, nil
}

// GetXRPLTokenByIssuerAndCurrency returns a XRPL registered token by issuer and currency or error.
func (c *ContractClient) GetXRPLTokenByIssuerAndCurrency(
	ctx context.Context,
	issuer, currency string,
) (XRPLToken, error) {
	tokens, err := c.GetXRPLTokens(ctx)
	if err != nil {
		return XRPLToken{}, err
	}
	for _, token := range tokens {
		if token.Issuer == issuer && token.Currency == currency {
			return token, nil
		}
	}

	return XRPLToken{}, errors.Errorf(
		"token not found in the registered tokens list, issuer:%s, currency:%s",
		issuer, currency,
	)
}

// GetXRPLTokens returns a list of all XRPL tokens.
func (c *ContractClient) GetXRPLTokens(ctx context.Context) ([]XRPLToken, error) {
	tokens := make([]XRPLToken, 0)
	lastKey := ""
	for {
		response, err := c.getPaginatedXRPLTokens(ctx, lastKey, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(response.Tokens) == 0 {
			break
		}
		tokens = append(tokens, response.Tokens...)
		lastKey = response.LastKey
	}

	return tokens, nil
}

// GetCoreumTokenByDenom returns a coreum registered token or nil by the provided denom.
func (c *ContractClient) GetCoreumTokenByDenom(ctx context.Context, denom string) (CoreumToken, error) {
	tokens, err := c.GetCoreumTokens(ctx)
	if err != nil {
		return CoreumToken{}, err
	}
	for _, token := range tokens {
		if token.Denom == denom {
			return token, nil
		}
	}

	return CoreumToken{}, errors.Errorf("token not found in the registered tokens list, denom:%s", denom)
}

// GetCoreumTokens returns a list of all coreum tokens.
func (c *ContractClient) GetCoreumTokens(ctx context.Context) ([]CoreumToken, error) {
	tokens := make([]CoreumToken, 0)
	lastKey := ""
	for {
		res, err := c.getPaginatedCoreumTokens(ctx, lastKey, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(res.Tokens) == 0 {
			break
		}
		tokens = append(tokens, res.Tokens...)
		lastKey = res.LastKey
	}

	return tokens, nil
}

// GetPendingOperations returns a list of all pending operations.
func (c *ContractClient) GetPendingOperations(ctx context.Context) ([]Operation, error) {
	operations := make([]Operation, 0)
	var startAfterKey *uint32
	for {
		res, err := c.getPaginatedPendingOperations(ctx, startAfterKey, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(res.Operations) == 0 {
			break
		}
		operations = append(operations, res.Operations...)
		startAfterKey = &res.LastKey
	}

	return operations, nil
}

// GetAvailableTickets returns a list of registered not used tickets.
func (c *ContractClient) GetAvailableTickets(ctx context.Context) ([]uint32, error) {
	var res availableTicketsResponse
	err := c.query(ctx, map[QueryMethod]struct{}{
		QueryMethodAvailableTickets: {},
	}, &res)
	if err != nil {
		return nil, err
	}

	return res.Tickets, nil
}

// GetFeesCollected returns collected fees for an account.
func (c *ContractClient) GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error) {
	var res feesCollectedResponse
	err := c.query(ctx, map[QueryMethod]any{
		QueryMethodFeesCollected: struct {
			RelayerAddress string `json:"relayer_address"`
		}{
			RelayerAddress: address.String(),
		},
	}, &res)
	if err != nil {
		return nil, err
	}

	return sdk.NewCoins(res.FeesCollected...), nil
}

// GetPendingRefunds returns the list of pending refunds for and address.
func (c *ContractClient) GetPendingRefunds(ctx context.Context, address sdk.AccAddress) ([]PendingRefund, error) {
	pendingRefunds := make([]PendingRefund, 0)
	var startAfterKey []string
	for {
		res, err := c.getPaginatedPendingRefunds(ctx, startAfterKey, &c.cfg.PageLimit, address)
		if err != nil {
			return nil, err
		}
		if len(res.PendingRefunds) == 0 {
			break
		}
		pendingRefunds = append(pendingRefunds, res.PendingRefunds...)
		startAfterKey = res.LastKey
	}

	return pendingRefunds, nil
}

// GetTransactionEvidences returns a list of transaction evidences.
func (c *ContractClient) GetTransactionEvidences(ctx context.Context) ([]TransactionEvidence, error) {
	transactionEvidences := make([]TransactionEvidence, 0)
	lastKey := ""
	for {
		res, err := c.getPaginatedTransactionEvidences(ctx, lastKey, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(res.TransactionEvidences) == 0 {
			break
		}
		transactionEvidences = append(transactionEvidences, res.TransactionEvidences...)
		lastKey = res.LastKey
	}

	return transactionEvidences, nil
}

// GetProhibitedXRPLAddresses returns the list prohibited XRPL addresses.
func (c *ContractClient) GetProhibitedXRPLAddresses(ctx context.Context) ([]string, error) {
	var response prohibitedXRPLAddressesResponse
	err := c.query(ctx, map[QueryMethod]any{
		QueryMethodProhibitedXRPLAddresses: struct{}{},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.ProhibitedXRPLAddresses, nil
}

// GetXRPLToCoreumTracingInfo returns XRPL to Coreum tracing info.
func (c *ContractClient) GetXRPLToCoreumTracingInfo(
	ctx context.Context,
	xrplTxHash string,
) (XRPLToCoreumTracingInfo, error) {
	txs, err := c.getContractTransactionsByWasmEventAttributes(ctx,
		map[string]string{
			eventAttributeAction: eventValueSaveAction,
			eventAttributeHash:   xrplTxHash,
		},
	)
	if err != nil {
		return XRPLToCoreumTracingInfo{}, err
	}

	xrplToCoreumTracingInfo := XRPLToCoreumTracingInfo{
		EvidenceToTxs: make([]DataToTx[XRPLToCoreumTransferEvidence], 0),
	}
	for _, tx := range txs {
		executePayloads, err := c.decodeExecutePayload(tx)
		if err != nil {
			return XRPLToCoreumTracingInfo{}, err
		}
		for i, payload := range executePayloads {
			if payload.SaveEvidence == nil || payload.SaveEvidence.Evidence.XRPLToCoreumTransfer == nil {
				continue
			}
			xrplToCoreumTracingInfo.EvidenceToTxs = append(
				xrplToCoreumTracingInfo.EvidenceToTxs,
				DataToTx[XRPLToCoreumTransferEvidence]{
					Evidence: *payload.SaveEvidence.Evidence.XRPLToCoreumTransfer,
					Tx:       tx,
				})
			if isEventValueEqual(tx.Logs[i].Events, wasmtypes.WasmModuleEventType, eventAttributeThresholdReached, "true") {
				xrplToCoreumTracingInfo.CoreumTx = tx
			}
		}
	}

	return xrplToCoreumTracingInfo, nil
}

// GetCoreumToXRPLTracingInfo returns Coreum to XRPL tracing info.
func (c *ContractClient) GetCoreumToXRPLTracingInfo(
	ctx context.Context,
	coreumTxHash string,
) (CoreumToXRPLTracingInfo, error) {
	txRes, err := c.cometServiceClient.GetTx(ctx, &sdktxtypes.GetTxRequest{
		Hash: coreumTxHash,
	})
	if err != nil {
		return CoreumToXRPLTracingInfo{}, err
	}
	if txRes == nil || txRes.TxResponse == nil {
		return CoreumToXRPLTracingInfo{}, errors.Errorf("tx with hash %s not found", coreumTxHash)
	}
	tx := txRes.TxResponse

	executePayloads, err := c.decodeExecutePayload(tx)
	if err != nil {
		return CoreumToXRPLTracingInfo{}, err
	}
	if len(executePayloads) == 0 {
		return CoreumToXRPLTracingInfo{}, errors.Errorf("the tx is not the WASM tx")
	}

	coreumToXRPLTracingInfo := CoreumToXRPLTracingInfo{
		CoreumTx:      *tx,
		XRPLTxHashes:  make([]string, 0),
		EvidenceToTxs: make([][]DataToTx[XRPLTransactionResultEvidence], 0),
	}

	for _, payload := range executePayloads {
		if payload.SendToXRPL == nil {
			continue
		}
		operationIDs, err := c.getSendToXRPLOperationIDs(ctx, *payload.SendToXRPL, tx.Height)
		if err != nil {
			return CoreumToXRPLTracingInfo{}, err
		}
		for _, operationID := range operationIDs {
			evidenceToTxs, xrplTxHashes, err := c.getXRPLTxsFromSaveTxResultEvidenceForOperation(ctx, operationID)
			if err != nil {
				return CoreumToXRPLTracingInfo{}, err
			}
			coreumToXRPLTracingInfo.EvidenceToTxs = append(coreumToXRPLTracingInfo.EvidenceToTxs, evidenceToTxs)
			coreumToXRPLTracingInfo.XRPLTxHashes = append(coreumToXRPLTracingInfo.XRPLTxHashes, xrplTxHashes...)
		}
	}

	return coreumToXRPLTracingInfo, err
}

func (c *ContractClient) getXRPLTxsFromSaveTxResultEvidenceForOperation(
	ctx context.Context,
	operationID uint32,
) ([]DataToTx[XRPLTransactionResultEvidence], []string, error) {
	txs, err := c.getContractTransactionsByWasmEventAttributes(ctx,
		map[string]string{
			eventAttributeAction:      eventValueSaveAction,
			eventAttributeOperationID: strconv.FormatUint(uint64(operationID), 10),
		},
	)
	if err != nil {
		return nil, nil, err
	}
	// find corresponding XRPL txs
	evidenceToTxs := make([]DataToTx[XRPLTransactionResultEvidence], 0)
	xrplTxHashes := make(map[string]struct{}, 0)
	for _, tx := range txs {
		executePayloads, err := c.decodeExecutePayload(tx)
		if err != nil {
			return nil, nil, err
		}
		for _, payload := range executePayloads {
			if payload.SaveEvidence == nil ||
				payload.SaveEvidence.Evidence.XRPLTransactionResult == nil {
				continue
			}
			evidenceToTxs = append(
				evidenceToTxs,
				DataToTx[XRPLTransactionResultEvidence]{
					Evidence: payload.SaveEvidence.Evidence.XRPLTransactionResult.XRPLTransactionResultEvidence,
					Tx:       tx,
				})
			xrplTxHashes[payload.SaveEvidence.Evidence.XRPLTransactionResult.TxHash] = struct{}{}
		}
	}

	return evidenceToTxs, lo.MapToSlice(xrplTxHashes, func(hash string, _ struct{}) string {
		return hash
	}), nil
}

func (c *ContractClient) getPaginatedXRPLTokens(
	ctx context.Context,
	startAfterKey string,
	limit *uint32,
) (xrplTokensResponse, error) {
	var response xrplTokensResponse
	err := c.query(ctx, map[QueryMethod]pagingStringKeyRequest{
		QueryMethodXRPLTokens: {
			StartAfterKey: startAfterKey,
			Limit:         limit,
		},
	}, &response)
	if err != nil {
		return response, err
	}

	return response, nil
}

func (c *ContractClient) getPaginatedCoreumTokens(
	ctx context.Context,
	startAfterKey string,
	limit *uint32,
) (coreumTokensResponse, error) {
	var response coreumTokensResponse
	err := c.query(ctx, map[QueryMethod]pagingStringKeyRequest{
		QueryMethodCoreumTokens: {
			StartAfterKey: startAfterKey,
			Limit:         limit,
		},
	}, &response)
	if err != nil {
		return response, err
	}

	return response, nil
}

func (c *ContractClient) getPaginatedPendingOperations(
	ctx context.Context,
	startAfterKey *uint32,
	limit *uint32,
) (pendingOperationsResponse, error) {
	var response pendingOperationsResponse
	err := c.query(ctx, map[QueryMethod]pagingUint32KeyRequest{
		QueryMethodPendingOperations: {
			StartAfterKey: startAfterKey,
			Limit:         limit,
		},
	}, &response)
	if err != nil {
		return pendingOperationsResponse{}, err
	}

	return response, nil
}

func (c *ContractClient) getPaginatedPendingRefunds(
	ctx context.Context,
	startAfterKey []string,
	limit *uint32,
	address sdk.AccAddress,
) (pendingRefundsResponse, error) {
	var res pendingRefundsResponse
	err := c.query(ctx, map[QueryMethod]pendingRefundsRequest{
		QueryMethodPendingRefunds: {
			StartAfterKey: startAfterKey,
			Limit:         limit,
			Address:       address,
		},
	}, &res)
	if err != nil {
		return pendingRefundsResponse{}, err
	}
	return res, nil
}

func (c *ContractClient) getPaginatedTransactionEvidences(
	ctx context.Context,
	startAfterKey string,
	limit *uint32,
) (transactionEvidencesResponse, error) {
	var res transactionEvidencesResponse
	err := c.query(ctx, map[QueryMethod]pagingStringKeyRequest{
		QueryMethodTransactionEvidences: {
			StartAfterKey: startAfterKey,
			Limit:         limit,
		},
	}, &res)
	if err != nil {
		return transactionEvidencesResponse{}, err
	}
	return res, nil
}

func (c *ContractClient) queryAssetFTIssueFee(ctx context.Context) (sdk.Coin, error) {
	assetFtParamsRes, err := c.assetftClient.Params(ctx, &assetfttypes.QueryParamsRequest{})
	if err != nil {
		return sdk.Coin{}, errors.Wrap(err, "failed to get asset ft issue fee")
	}

	return assetFtParamsRes.Params.IssueFee, nil
}

func (c *ContractClient) execute(
	ctx context.Context,
	sender sdk.AccAddress,
	requests ...execRequest,
) (*sdk.TxResponse, error) {
	if c.cfg.ContractAddress == nil {
		return nil, errors.New("failed to execute with empty contract address")
	}
	c.execMu.Lock()
	defer c.execMu.Unlock()

	msgs := make([]sdk.Msg, 0, len(requests))
	for _, req := range requests {
		payload, err := json.Marshal(req.Body)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to marshal payload, request:%+v", req.Body)
		}
		c.log.Debug(ctx, "Executing contract", zap.String("payload", string(payload)))
		msg := &wasmtypes.MsgExecuteContract{
			Sender:   sender.String(),
			Contract: c.cfg.ContractAddress.String(),
			Msg:      payload,
			Funds:    req.Funds,
		}
		msgs = append(msgs, msg)
	}

	clientCtx := c.clientCtx.WithFromAddress(sender)
	if clientCtx.GenerateOnly() {
		unsignedTx, err := client.GenerateUnsignedTx(ctx, clientCtx, c.getTxFactory(), msgs...)
		if err != nil {
			return nil, err
		}

		txData, err := clientCtx.TxConfig().TxJSONEncoder()(unsignedTx.GetTx())
		if err != nil {
			return nil, err
		}

		return nil, clientCtx.PrintString(fmt.Sprintf("%s\n", txData))
	}

	var res *sdk.TxResponse
	outOfGasRetryAttempt := uint32(1)
	err := retry.Do(ctx, c.cfg.OutOfGasRetryDelay, func() error {
		var err error
		res, err = client.BroadcastTx(ctx, clientCtx.WithFromAddress(sender), c.getTxFactory(), msgs...)
		if err == nil {
			return nil
		}
		// stop if we have reached the max retires
		if outOfGasRetryAttempt >= c.cfg.OutOfGasRetryAttempts {
			return err
		}
		if cosmoserrors.ErrOutOfGas.Is(err) {
			outOfGasRetryAttempt++
			c.log.Info(ctx, "Out of gas, retrying Coreum tx execution")
			return retry.Retryable(errors.Wrapf(err, "retry tx execution, out of gas"))
		}

		return errors.Wrapf(err, "failed to execute transaction, message:%+v", msgs)
	})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *ContractClient) query(ctx context.Context, request, response any) error {
	if c.cfg.ContractAddress == nil {
		return errors.New("failed to execute with empty contract address")
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal query request")
	}
	c.log.Debug(ctx, "Querying contract", zap.String("payload", string(payload)))

	query := &wasmtypes.QuerySmartContractStateRequest{
		Address:   c.cfg.ContractAddress.String(),
		QueryData: payload,
	}
	resp, err := c.wasmClient.SmartContractState(ctx, query)
	if err != nil {
		return errors.Wrapf(err, "query failed, request:%+v", request)
	}

	c.log.Debug(ctx, "Query is succeeded", zap.String("data", string(resp.Data)))
	if err := json.Unmarshal(resp.Data, response); err != nil {
		return errors.Wrapf(
			err,
			"failed to unmarshal wasm contract response, request:%s, response:%s",
			string(payload),
			string(resp.Data),
		)
	}

	return nil
}

func (c *ContractClient) getTxFactory() client.Factory {
	return client.Factory{}.
		WithKeybase(c.clientCtx.Keyring()).
		WithChainID(c.clientCtx.ChainID()).
		WithTxConfig(c.clientCtx.TxConfig()).
		WithMemo(fmt.Sprintf("%s %s", RelayerCoreumMemoPrefix, buildinfo.VersionTag)).
		WithSimulateAndExecute(true)
}

func (c *ContractClient) getContractTransactionsByWasmEventAttributes(
	ctx context.Context,
	attributes map[string]string,
) ([]*sdk.TxResponse, error) {
	page := uint64(0)
	txResponses := make([]*sdk.TxResponse, 0)
	events := []string{
		fmt.Sprintf(
			"%s.%s='%s'",
			wasmtypes.WasmModuleEventType,
			wasmtypes.AttributeKeyContractAddr,
			c.GetContractAddress().String(),
		),
	}
	for key, value := range attributes {
		events = append(events, fmt.Sprintf(
			"%s.%s='%s'",
			wasmtypes.WasmModuleEventType,
			key,
			value,
		))
	}

	attributes[wasmtypes.AttributeKeyContractAddr] = wasmtypes.WasmModuleEventType
	for {
		txEventsPage, err := c.cometServiceClient.GetTxsEvent(ctx, &sdktxtypes.GetTxsEventRequest{
			Events:  events,
			OrderBy: sdktxtypes.OrderBy_ORDER_BY_DESC,
			Page:    page,
			Limit:   uint64(c.cfg.TxsQueryPageLimit),
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get contrac txs by events")
		}
		txResponses = append(txResponses, txEventsPage.TxResponses...)
		if len(txEventsPage.TxResponses) < int(c.cfg.TxsQueryPageLimit) {
			break
		}
		page++
	}

	return txResponses, nil
}

func (c *ContractClient) decodeExecutePayload(txAny *sdk.TxResponse) ([]ExecutePayload, error) {
	var tx sdk.Tx
	if err := c.clientCtx.Codec().UnpackAny(txAny.Tx, &tx); err != nil {
		return nil, errors.Errorf("failed to unpack sdk.Tx, tx:%v", tx)
	}

	executePayloads := make([]ExecutePayload, 0)
	for _, msg := range tx.GetMsgs() {
		executeContractMsg, ok := msg.(*wasmtypes.MsgExecuteContract)
		if !ok {
			continue
		}
		payload := executeContractMsg.Msg
		var executePayload ExecutePayload
		if err := json.Unmarshal(payload, &executePayload); err != nil {
			return nil, errors.Wrapf(err, "failed to decode contract payload to map, raw payload:%s, tx:%v", string(payload), tx)
		}
		executePayloads = append(executePayloads, executePayload)
	}

	return executePayloads, nil
}

func isEventValueEqual(
	events sdk.StringEvents,
	etype, key, value string,
) bool {
	for _, ev := range events {
		if ev.Type != etype {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key != key {
				continue
			}

			return attr.Value == value
		}
	}
	return false
}

func (c *ContractClient) getSendToXRPLOperationIDs(
	ctx context.Context,
	sendReq SendToXRPLRequest,
	txHeight int64,
) ([]uint32, error) {
	beforeCtx := WithHeightRequestContext(ctx, txHeight-1)
	operationsBefore, err := c.GetPendingOperations(beforeCtx)
	if err != nil {
		return nil, err
	}

	afterCtx := WithHeightRequestContext(ctx, txHeight)
	operationsAfter, err := c.GetPendingOperations(afterCtx)
	if err != nil {
		return nil, err
	}

	operationsBeforeMap := lo.SliceToMap(operationsBefore, func(operation Operation) (uint32, Operation) {
		return operation.GetOperationID(), operation
	})

	operationIDs := make([]uint32, 0)
	for _, operation := range operationsAfter {
		if _, ok := operationsBeforeMap[operation.GetOperationID()]; !ok {
			if operation.OperationType.CoreumToXRPLTransfer == nil {
				continue
			}
			if operation.OperationType.CoreumToXRPLTransfer.Recipient != sendReq.Recipient {
				continue
			}
			operationIDs = append(operationIDs, operation.GetOperationID())
		}
	}

	return operationIDs, nil
}

// ******************** Context ********************

// WithHeightRequestContext adds the height to the context for queries.
func WithHeightRequestContext(ctx context.Context, height int64) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpctypes.GRPCBlockHeightHeader,
		strconv.FormatInt(height, 10),
	)
}

// ******************** Contract error ********************

// IsNotOwnerError returns true if error is `not owner`.
func IsNotOwnerError(err error) bool {
	return isError(err, "Caller is not the contract's current owner")
}

// IsCoreumTokenAlreadyRegisteredError returns true if error is `CoreumTokenAlreadyRegistered`.
func IsCoreumTokenAlreadyRegisteredError(err error) bool {
	return isError(err, "CoreumTokenAlreadyRegistered")
}

// IsXRPLTokenAlreadyRegisteredError returns true if error is `XRPLTokenAlreadyRegistered`.
func IsXRPLTokenAlreadyRegisteredError(err error) bool {
	return isError(err, "XRPLTokenAlreadyRegistered")
}

// IsUnauthorizedSenderError returns true if error is `UnauthorizedSender`.
func IsUnauthorizedSenderError(err error) bool {
	return isError(err, "UnauthorizedSender")
}

// IsOperationAlreadyExecutedError returns true if error is `OperationAlreadyExecuted`.
func IsOperationAlreadyExecutedError(err error) bool {
	return isError(err, "OperationAlreadyExecuted")
}

// IsTokenNotRegisteredError returns true if error is `TokenNotRegistered`.
func IsTokenNotRegisteredError(err error) bool {
	return isError(err, "TokenNotRegistered")
}

// IsEvidenceAlreadyProvidedError returns true if error is `EvidenceAlreadyProvided`.
func IsEvidenceAlreadyProvidedError(err error) bool {
	return isError(err, "EvidenceAlreadyProvided")
}

// IsPendingTicketUpdateError returns true if error is `PendingTicketUpdate`.
func IsPendingTicketUpdateError(err error) bool {
	return isError(err, "PendingTicketUpdate")
}

// IsInvalidTicketSequenceToAllocateError returns true if error is `InvalidTicketSequenceToAllocate`.
func IsInvalidTicketSequenceToAllocateError(err error) bool {
	return isError(err, "InvalidTicketSequenceToAllocate")
}

// IsSignatureAlreadyProvidedError returns true if error is `SignatureAlreadyProvided`.
func IsSignatureAlreadyProvidedError(err error) bool {
	return isError(err, "SignatureAlreadyProvided")
}

// IsPendingOperationNotFoundError returns true if error is `PendingOperationNotFound`.
func IsPendingOperationNotFoundError(err error) bool {
	return isError(err, "PendingOperationNotFound")
}

// IsAmountSentIsZeroAfterTruncationError returns true if error is `AmountSentIsZeroAfterTruncation`.
func IsAmountSentIsZeroAfterTruncationError(err error) bool {
	return isError(err, "AmountSentIsZeroAfterTruncation")
}

// IsMaximumBridgedAmountReachedError returns true if error is `MaximumBridgedAmountReached`.
func IsMaximumBridgedAmountReachedError(err error) bool {
	return isError(err, "MaximumBridgedAmountReached")
}

// IsStillHaveAvailableTicketsError returns true if error is `StillHaveAvailableTickets`.
func IsStillHaveAvailableTicketsError(err error) bool {
	return isError(err, "StillHaveAvailableTickets")
}

// IsTokenNotEnabledError returns true if error is `TokenNotEnabled`.
func IsTokenNotEnabledError(err error) bool {
	return isError(err, "TokenNotEnabled")
}

// IsInvalidXRPLAddressError returns true if error is `InvalidXRPLAddress`.
func IsInvalidXRPLAddressError(err error) bool {
	return isError(err, "InvalidXRPLAddress")
}

// IsLastTicketReservedError returns true if error is `LastTicketReserved`.
func IsLastTicketReservedError(err error) bool {
	return isError(err, "LastTicketReserved")
}

// IsNoAvailableTicketsError returns true if error is `NoAvailableTickets`.
func IsNoAvailableTicketsError(err error) bool {
	return isError(err, "NoAvailableTickets")
}

// IsXRPLTokenNotInactiveError returns true if error is `XRPLTokenNotInactive`.
func IsXRPLTokenNotInactiveError(err error) bool {
	return isError(err, "XRPLTokenNotInactive")
}

// IsTokenStateIsImmutableError returns true if error is `TokenStateIsImmutable`.
func IsTokenStateIsImmutableError(err error) bool {
	return isError(err, "TokenStateIsImmutable")
}

// IsInvalidTargetTokenStateError returns true if error is `InvalidTargetTokenState`.
func IsInvalidTargetTokenStateError(err error) bool {
	return isError(err, "InvalidTargetTokenState")
}

// IsBridgeHaltedError returns true if error is `BridgeHalted`.
func IsBridgeHaltedError(err error) bool {
	return isError(err, "BridgeHalted")
}

// IsRotateKeysOngoingError returns true if error is `RotateKeysOngoing`.
func IsRotateKeysOngoingError(err error) bool {
	return isError(err, "RotateKeysOngoing")
}

// IsInvalidTargetMaxHoldingAmountError returns true if error is `InvalidTargetMaxHoldingAmount`.
func IsInvalidTargetMaxHoldingAmountError(err error) bool {
	return isError(err, "InvalidTargetMaxHoldingAmount")
}

// IsInvalidDeliverAmountError returns true if error is `InvalidDeliverAmount`.
func IsInvalidDeliverAmountError(err error) bool {
	return isError(err, "InvalidDeliverAmount")
}

// IsDeliverAmountIsProhibitedError returns true if error is `DeliverAmountIsProhibited`.
func IsDeliverAmountIsProhibitedError(err error) bool {
	return isError(err, "DeliverAmountIsProhibited")
}

// IsOperationVersionMismatchError returns true if error is `OperationVersionMismatch`.
func IsOperationVersionMismatchError(err error) bool {
	return isError(err, "OperationVersionMismatch")
}

// IsProhibitedAddressError returns true if error is `ProhibitedAddress`.
func IsProhibitedAddressError(err error) bool {
	return isError(err, "ProhibitedAddress")
}

// IsCannotCoverBridgingFeesError returns true if error is `CannotCoverBridgingFees`.
func IsCannotCoverBridgingFeesError(err error) bool {
	return isError(err, "CannotCoverBridgingFees")
}

// IsInvalidOperationResultError returns true if error is `InvalidOperationResult`.
func IsInvalidOperationResultError(err error) bool {
	return isError(err, "InvalidOperationResult")
}

// IsInvalidTransactionResultEvidenceError returns true if error is `InvalidTransactionResultEvidence`.
func IsInvalidTransactionResultEvidenceError(err error) bool {
	return isError(err, "InvalidTransactionResultEvidence")
}

// IsInvalidSuccessfulTransactionResultEvidenceError returns true if error is
// `InvalidSuccessfulTransactionResultEvidence`.
func IsInvalidSuccessfulTransactionResultEvidenceError(err error) bool {
	return isError(err, "InvalidSuccessfulTransactionResultEvidence")
}

// IsInvalidFailedTransactionResultEvidenceError returns true if error is `InvalidFailedTransactionResultEvidence`.
func IsInvalidFailedTransactionResultEvidenceError(err error) bool {
	return isError(err, "InvalidFailedTransactionResultEvidence")
}

// IsInvalidTicketAllocationEvidenceError returns true if error is `InvalidTicketAllocationEvidence`.
func IsInvalidTicketAllocationEvidenceError(err error) bool {
	return isError(err, "InvalidTicketAllocationEvidence")
}

// ******************** Asset FT errors ********************

// IsAssetFTStateError returns true if the error is caused by enabled asset FT features.
func IsAssetFTStateError(err error) bool {
	return IsAssetFTFreezingError(err) ||
		IsAssetFTGlobalFreezingError(err) ||
		IsAssetFTWhitelistedLimitExceededError(err)
}

// IsAssetFTFreezingError returns true if error is cause of the lack of freezing amount.
func IsAssetFTFreezingError(err error) bool {
	return isError(err, "is not available, available") && isError(err, "insufficient funds")
}

// IsAssetFTGlobalFreezingError returns true if error is cause is token global freeze.
func IsAssetFTGlobalFreezingError(err error) bool {
	return isError(err, "token is globally frozen")
}

// IsAssetFTWhitelistedLimitExceededError returns true if error is whitelisted limit exceeded.
func IsAssetFTWhitelistedLimitExceededError(err error) bool {
	return isError(err, "whitelisted limit exceeded")
}

// IsRecipientBlockedError returns true if error is the recipient is blocked.
func IsRecipientBlockedError(err error) bool {
	return isError(err, "is not allowed to receive funds: unauthorized")
}

func isError(err error, errorString string) bool {
	return err != nil && strings.Contains(err.Error(), errorString)
}
