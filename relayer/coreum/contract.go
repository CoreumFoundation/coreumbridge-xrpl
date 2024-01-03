//nolint:tagliatelle // contract spec
package coreum

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
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
	ExecUpdateXRPLToken               ExecMethod = "update_xrpl_token"
	ExecUpdateCoreumToken             ExecMethod = "update_coreum_token"
	ExecClaimRefunds                  ExecMethod = "claim_refunds"
)

// TransactionResult is transaction result.
type TransactionResult string

// TransactionResult values.
const (
	TransactionResultAccepted TransactionResult = "accepted"
	TransactionResultRejected TransactionResult = "rejected"
	TransactionResultInvalid  TransactionResult = "invalid"
)

// TokenState is transaction result.
type TokenState string

// TokenState values.
const (
	TokenStateEnabled    TokenState = "enabled"
	TokenStateDisabled   TokenState = "disabled"
	TokenStateProcessing TokenState = "processing"
	TokenStateInactive   TokenState = "inactive"
)

// QueryMethod is contract query method.
type QueryMethod string

// QueryMethods.
const (
	QueryMethodConfig            QueryMethod = "config"
	QueryMethodOwnership         QueryMethod = "ownership"
	QueryMethodXRPLTokens        QueryMethod = "xrpl_tokens"
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
	EvidenceThreshold           int
	UsedTicketSequenceThreshold int
	TrustSetLimitAmount         sdkmath.Int
	BridgeXRPLAddress           string
}

// ContractConfig is contract config.
type ContractConfig struct {
	Relayers                    []Relayer   `json:"relayers"`
	EvidenceThreshold           int         `json:"evidence_threshold"`
	UsedTicketSequenceThreshold int         `json:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         sdkmath.Int `json:"trust_set_limit_amount"`
	BridgeXRPLAddress           string      `json:"bridge_xrpl_address"`
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
	// we don't use the tag here since have we don't use that struct as transport object
	Tickets []uint32
}

// XRPLTransactionResultTrustSetEvidence is evidence of the trust set transaction.
type XRPLTransactionResultTrustSetEvidence struct {
	XRPLTransactionResultEvidence
	// we don't use the tag here since have we don't use that struct as transport object
	Issuer   string
	Currency string
}

// XRPLTransactionResultCoreumToXRPLTransferEvidence is evidence of the sending from XRPL to coreum.
type XRPLTransactionResultCoreumToXRPLTransferEvidence struct {
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
	Issuer    string      `json:"issuer"`
	Currency  string      `json:"currency"`
	Amount    sdkmath.Int `json:"amount"`
	Recipient string      `json:"recipient"`
}

// OperationType is operation type.
type OperationType struct {
	AllocateTickets      *OperationTypeAllocateTickets      `json:"allocate_tickets,omitempty"`
	TrustSet             *OperationTypeTrustSet             `json:"trust_set,omitempty"`
	CoreumToXRPLTransfer *OperationTypeCoreumToXRPLTransfer `json:"coreum_to_xrpl_transfer,omitempty"`
}

// Operation is contract operation which should be signed and executed.
type Operation struct {
	TicketSequence  uint32        `json:"ticket_sequence"`
	AccountSequence uint32        `json:"account_sequence"`
	Signatures      []Signature   `json:"signatures"`
	OperationType   OperationType `json:"operation_type"`
}

// GetOperationID returns operation ID.
func (o Operation) GetOperationID() uint32 {
	if o.TicketSequence != 0 {
		return o.TicketSequence
	}

	return o.AccountSequence
}

// ******************** Internal transport object  ********************

type instantiateRequest struct {
	Owner                       sdk.AccAddress `json:"owner"`
	Relayers                    []Relayer      `json:"relayers"`
	EvidenceThreshold           int            `json:"evidence_threshold"`
	UsedTicketSequenceThreshold int            `json:"used_ticket_sequence_threshold"`
	TrustSetLimitAmount         sdkmath.Int    `json:"trust_set_limit_amount"`
	BridgeXRPLAddress           string         `json:"bridge_xrpl_address"`
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

type saveSignatureRequest struct {
	OperationID uint32 `json:"operation_id"`
	Signature   string `json:"signature"`
}

type sendToXRPLRequest struct {
	Recipient string `json:"recipient"`
}

type recoverXRPLTokenRegistrationRequest struct {
	Issuer   string `json:"issuer"`
	Currency string `json:"currency"`
}

type updateXRPLTokenRequest struct {
	Issuer   string     `json:"issuer"`
	Currency string     `json:"currency"`
	State    TokenState `json:"state"`
}

type updateCoreumTokenRequest struct {
	Denom string     `json:"denom"`
	State TokenState `json:"state"`
}

type claimRefundsRequest struct {
	PendingOperationID string `json:"pending_operation_id"`
}

type xrplTransactionEvidenceTicketsAllocationOperationResult struct {
	Tickets []uint32 `json:"tickets"`
}

type xrplTransactionEvidenceTrustSetOperationResult struct {
	Issuer   string `json:"issuer"`
	Currency string `json:"currency"`
}

type xrplTransactionEvidenceCoreumToXRPLTransferOperationResult struct{}

//nolint:lll // breaking this down will make it less readable.
type xrplTransactionEvidenceOperationResult struct {
	TicketsAllocation    *xrplTransactionEvidenceTicketsAllocationOperationResult    `json:"tickets_allocation,omitempty"`
	TrustSet             *xrplTransactionEvidenceTrustSetOperationResult             `json:"trust_set,omitempty"`
	CoreumToXRPLTransfer *xrplTransactionEvidenceCoreumToXRPLTransferOperationResult `json:"coreum_to_xrpl_transfer,omitempty"`
}

type xrplTransactionResultEvidence struct {
	XRPLTransactionResultEvidence
	OperationResult xrplTransactionEvidenceOperationResult `json:"operation_result"`
}

type evidence struct {
	XRPLToCoreumTransfer  *XRPLToCoreumTransferEvidence  `json:"xrpl_to_coreum_transfer,omitempty"`
	XRPLTransactionResult *xrplTransactionResultEvidence `json:"xrpl_transaction_result,omitempty"`
}

type xrplTokensResponse struct {
	Tokens []XRPLToken `json:"tokens"`
}

type coreumTokensResponse struct {
	Tokens []CoreumToken `json:"tokens"`
}

type pendingOperationsResponse struct {
	Operations []Operation `json:"operations"`
}

type availableTicketsResponse struct {
	Tickets []uint32 `json:"tickets"`
}

type pendingRefundsResponse struct {
	PendingRefunds []PendingRefund `json:"pending_refunds"`
}

type PendingRefund struct {
	ID   string   `json:"id"`
	Coin sdk.Coin `json:"coin"`
}

type pagingRequest struct {
	Offset *uint64 `json:"offset"`
	Limit  *uint32 `json:"limit"`
}

type execRequest struct {
	Body  any
	Funds sdk.Coins
}

// ******************** Client ********************

// ContractClientConfig represent the ContractClient config.
type ContractClientConfig struct {
	ContractAddress    sdk.AccAddress
	GasAdjustment      float64
	GasPriceAdjustment sdk.Dec
	PageLimit          uint32
}

// DefaultContractClientConfig returns default ContractClient config.
func DefaultContractClientConfig(contractAddress sdk.AccAddress) ContractClientConfig {
	return ContractClientConfig{
		ContractAddress:    contractAddress,
		GasAdjustment:      2,
		GasPriceAdjustment: sdk.MustNewDecFromStr("1.2"),
		PageLimit:          250,
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
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]registerCoreumTokenRequest{
			ExecMethodRegisterCoreumToken: {
				Denom:            denom,
				Decimals:         decimals,
				SendingPrecision: sendingPrecision,
				MaxHoldingAmount: maxHoldingAmount,
				BridgingFee:      sdkmath.NewInt(0),
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
				BridgingFee:      sdkmath.NewInt(0),
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
				OperationResult: xrplTransactionEvidenceOperationResult{
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
				OperationResult: xrplTransactionEvidenceOperationResult{
					TrustSet: &xrplTransactionEvidenceTrustSetOperationResult{
						Issuer:   evd.Issuer,
						Currency: evd.Currency,
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
				OperationResult: xrplTransactionEvidenceOperationResult{
					CoreumToXRPLTransfer: &xrplTransactionEvidenceCoreumToXRPLTransferOperationResult{},
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
	signature string,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]saveSignatureRequest{
			ExecMethodSaveSignature: {
				OperationID: operationID,
				Signature:   signature,
			},
		},
	})
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
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]sendToXRPLRequest{
			ExecSendToXRPL: {
				Recipient: recipient,
			},
		},
		Funds: sdk.NewCoins(amount),
	})
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

// UpdateXRPLToken executes `update_xrpl_token` method.
func (c *ContractClient) UpdateXRPLToken(
	ctx context.Context,
	sender sdk.AccAddress,
	issuer, currency string,
	state TokenState,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateXRPLTokenRequest{
			ExecUpdateXRPLToken: {
				Issuer:   issuer,
				Currency: currency,
				State:    state,
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
	state TokenState,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]updateCoreumTokenRequest{
			ExecUpdateCoreumToken: {
				Denom: denom,
				State: state,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// ClaimRefunds executes `claim_refunds` method.
func (c *ContractClient) ClaimRefunds(
	ctx context.Context,
	sender sdk.AccAddress,
	pendingOperationID string,
) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]claimRefundsRequest{
			ExecClaimRefunds: {
				PendingOperationID: pendingOperationID,
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
	offset := uint64(0)
	for {
		pageTokens, err := c.getPaginatedXRPLTokens(ctx, &offset, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(pageTokens) == 0 {
			break
		}
		tokens = append(tokens, pageTokens...)
		offset += uint64(c.cfg.PageLimit)
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
	offset := uint64(0)
	for {
		pageTokens, err := c.getPaginatedCoreumTokens(ctx, &offset, &c.cfg.PageLimit)
		if err != nil {
			return nil, err
		}
		if len(pageTokens) == 0 {
			break
		}
		tokens = append(tokens, pageTokens...)
		offset += uint64(c.cfg.PageLimit)
	}

	return tokens, nil
}

// GetPendingOperations returns a list of all pending operations.
func (c *ContractClient) GetPendingOperations(ctx context.Context) ([]Operation, error) {
	var response pendingOperationsResponse
	err := c.query(ctx, map[QueryMethod]struct{}{
		QueryMethodPendingOperations: {},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.Operations, nil
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

// GetAvailableTickets returns a list of registered not used tickets.
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

func (c *ContractClient) getPaginatedXRPLTokens(
	ctx context.Context,
	offset *uint64,
	limit *uint32,
) ([]XRPLToken, error) {
	var response xrplTokensResponse
	err := c.query(ctx, map[QueryMethod]pagingRequest{
		QueryMethodXRPLTokens: {
			Offset: offset,
			Limit:  limit,
		},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.Tokens, nil
}

func (c *ContractClient) getPaginatedCoreumTokens(
	ctx context.Context,
	offset *uint64,
	limit *uint32,
) ([]CoreumToken, error) {
	var response coreumTokensResponse
	err := c.query(ctx, map[QueryMethod]pagingRequest{
		QueryMethodCoreumTokens: {
			Offset: offset,
			Limit:  limit,
		},
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.Tokens, nil
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

	res, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msgs...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute transaction, message:%+v", msgs)
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

func isError(err error, errorString string) bool {
	return err != nil && strings.Contains(err.Error(), errorString)
}
