//nolint:tagliatelle // contract spec
package coreum

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

const (
	contractLabel = "coreumbridge-xrpl"
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
	QueryMethodConfig            QueryMethod = "config"
	QueryMethodOwnership         QueryMethod = "ownership"
	QueryMethodXRPLTokens        QueryMethod = "xrpl_tokens"
	QueryMethodFeesCollected     QueryMethod = "fees_collected"
	QueryMethodCoreumTokens      QueryMethod = "coreum_tokens"
	QueryMethodPendingOperations QueryMethod = "pending_operations"
	QueryMethodAvailableTickets  QueryMethod = "available_tickets"
	QueryMethodPendingRefunds    QueryMethod = "pending_refunds"
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
	Recipient     string
	Amount        sdk.Coin
	DeliverAmount *sdkmath.Int
}

// SaveSignatureRequest defines single request to save relayer signature.
type SaveSignatureRequest struct {
	OperationID      uint32
	OperationVersion uint32
	Signature        string
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

type saveEvidenceRequest struct {
	Evidence evidence `json:"evidence"`
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

type sendToXRPLRequest struct {
	DeliverAmount *sdkmath.Int `json:"deliver_amount,omitempty"`
	Recipient     string       `json:"recipient"`
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

type pendingRefundsResponse struct {
	PendingRefunds []PendingRefund `json:"pending_refunds"`
}

// PendingRefund holds the pending refund information.
type PendingRefund struct {
	ID         string   `json:"id"`
	Coin       sdk.Coin `json:"coin"`
	XRPLTxHash string   `json:"xrpl_tx_hash"`
}

type pagingStringKeyRequest struct {
	StartAfterKey string  `json:"start_after_key,omitempty"`
	Limit         *uint32 `json:"limit"`
}

type pagingUint32KeyRequest struct {
	StartAfterKey *uint32 `json:"start_after_key,omitempty"`
	Limit         *uint32 `json:"limit"`
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
	GasPriceAdjustment    sdk.Dec
	PageLimit             uint32
	OutOfGasRetryDelay    time.Duration
	OutOfGasRetryAttempts uint32
}

// DefaultContractClientConfig returns default ContractClient config.
func DefaultContractClientConfig(contractAddress sdk.AccAddress) ContractClientConfig {
	return ContractClientConfig{
		ContractAddress:       contractAddress,
		GasAdjustment:         1.4,
		GasPriceAdjustment:    sdk.MustNewDecFromStr("1.2"),
		PageLimit:             50,
		OutOfGasRetryDelay:    500 * time.Millisecond,
		OutOfGasRetryAttempts: 5,
	}
}

// ContractClient is the bridge contract client.
type ContractClient struct {
	cfg           ContractClientConfig
	log           logger.Logger
	clientCtx     client.Context
	wasmClient    wasmtypes.QueryClient
	assetftClient assetfttypes.QueryClient

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
		wasmClient:    wasmtypes.NewQueryClient(clientCtx),
		assetftClient: assetfttypes.NewQueryClient(clientCtx),

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
	msgStoreCode := &wasmtypes.MsgStoreCode{
		Sender:       sender.String(),
		WASMByteCode: byteCode,
	}
	c.log.Info(ctx, "Deploying contract bytecode.")

	res, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msgStoreCode)
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy wasm bytecode")
	}
	codeID, err := event.FindUint64EventAttribute(res.Events, wasmtypes.EventTypeStoreCode, wasmtypes.AttributeKeyCodeID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find code ID in the tx result")
	}
	c.log.Info(ctx, "The contract bytecode is deployed.", zap.Uint64("codeID", codeID))

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
	res, err = client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msg)
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
	req := saveEvidenceRequest{
		Evidence: evidence{
			XRPLToCoreumTransfer: &evd,
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]saveEvidenceRequest{
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
	req := saveEvidenceRequest{
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
		Body: map[ExecMethod]saveEvidenceRequest{
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
	req := saveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]saveEvidenceRequest{
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
	req := saveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]saveEvidenceRequest{
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
	req := saveEvidenceRequest{
		Evidence: evidence{
			XRPLTransactionResult: &xrplTransactionResultEvidence{
				XRPLTransactionResultEvidence: evd.XRPLTransactionResultEvidence,
			},
		},
	}
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]saveEvidenceRequest{
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
			Body: map[ExecMethod]sendToXRPLRequest{
				ExecSendToXRPL: {
					DeliverAmount: req.DeliverAmount,
					Recipient:     req.Recipient,
				},
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
		response, err := c.getPaginatedCoreumTokens(ctx, lastKey, &c.cfg.PageLimit)
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

// GetPendingOperations returns a list of all pending operations.
func (c *ContractClient) GetPendingOperations(ctx context.Context) ([]Operation, error) {
	operations := make([]Operation, 0)
	var startAfterKey *uint32
	for {
		response, err := c.getPaginatedPendingOperations(ctx, startAfterKey, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(response.Operations) == 0 {
			break
		}
		operations = append(operations, response.Operations...)
		startAfterKey = &response.LastKey
	}

	return operations, nil
}

// GetAvailableTickets returns a list of registered not used tickets.
func (c *ContractClient) GetAvailableTickets(ctx context.Context) ([]uint32, error) {
	var response availableTicketsResponse
	err := c.query(ctx, map[QueryMethod]struct{}{
		QueryMethodAvailableTickets: {},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.Tickets, nil
}

// GetFeesCollected returns collected fees for an account.
func (c *ContractClient) GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error) {
	var response feesCollectedResponse
	err := c.query(ctx, map[QueryMethod]interface{}{
		QueryMethodFeesCollected: struct {
			RelayerAddress string `json:"relayer_address"`
		}{
			RelayerAddress: address.String(),
		},
	}, &response)
	if err != nil {
		return nil, err
	}

	return sdk.NewCoins(response.FeesCollected...), nil
}

// GetPendingRefunds returns the list of pending refunds for and address.
func (c *ContractClient) GetPendingRefunds(ctx context.Context, address sdk.AccAddress) ([]PendingRefund, error) {
	var response pendingRefundsResponse
	err := c.query(ctx, map[QueryMethod]interface{}{
		QueryMethodPendingRefunds: struct {
			Address string `json:"address"`
		}{
			Address: address.String(),
		},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.PendingRefunds, nil
}

// SetGenerateOnly sets the client.Context.GenerateOnly.
func (c *ContractClient) SetGenerateOnly(generateOnly bool) {
	c.clientCtx = c.clientCtx.WithGenerateOnly(generateOnly)
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

		json, err := clientCtx.TxConfig().TxJSONEncoder()(unsignedTx.GetTx())
		if err != nil {
			return nil, err
		}

		return nil, clientCtx.PrintString(fmt.Sprintf("%s\n", json))
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
			c.log.Warn(ctx, "Out of gas, retying Coreum tx execution, out of gas")
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
		WithMemo(fmt.Sprintf("relayer_version:%s", buildinfo.VersionTag)).
		WithSimulateAndExecute(true)
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

// IsProhibitedRecipientError returns true if error is `ProhibitedRecipient`.
func IsProhibitedRecipientError(err error) bool {
	return isError(err, "ProhibitedRecipient")
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
