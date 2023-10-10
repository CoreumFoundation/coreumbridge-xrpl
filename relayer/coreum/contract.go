//nolint:tagliatelle // contract spec
package coreum

import (
	"context"
	"encoding/json"
	"strings"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum/v3/pkg/client"
	"github.com/CoreumFoundation/coreum/v3/testutil/event"
	assetfttypes "github.com/CoreumFoundation/coreum/v3/x/asset/ft/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

const (
	contractLabel = "coreumbridge-xrpl"
)

// ExecMethod is contract exec method.
type ExecMethod string

// ExecMethods.
const (
	ExecMethodUpdateOwnership     ExecMethod = "update_ownership"
	ExecMethodRegisterCoreumToken ExecMethod = "register_coreum_token"
	ExecMethodRegisterXRPLToken   ExecMethod = "register_x_r_p_l_token"
)

// QueryMethod is contract query method.
type QueryMethod string

// QueryMethods.
const (
	QueryMethodConfig       QueryMethod = "config"
	QueryMethodOwnership    QueryMethod = "ownership"
	QueryMethodXRPLTokens   QueryMethod = "x_r_p_l_tokens"
	QueryMethodCoreumTokens QueryMethod = "coreum_tokens"
)

const (
	notOwnerErrorString                     = "Caller is not the contract's current owner"
	coreumTokenAlreadyRegisteredErrorString = "CoreumTokenAlreadyRegistered"
	xrplTokenAlreadyRegisteredErrorString   = "XRPLTokenAlreadyRegistered"
)

// InstantiationConfig holds attributes used for the contract instantiation.
type InstantiationConfig struct {
	Owner                sdk.AccAddress
	Admin                sdk.AccAddress
	Relayers             []sdk.AccAddress
	EvidenceThreshold    int
	UsedTicketsThreshold int
}

// ContractConfig is contract config.
type ContractConfig struct {
	Relayers             []sdk.AccAddress `json:"relayers"`
	EvidenceThreshold    int              `json:"evidence_threshold"`
	UsedTicketsThreshold int              `json:"used_tickets_threshold"`
}

// ContractOwnership is owner contract config.
type ContractOwnership struct {
	Owner        sdk.AccAddress `json:"owner"`
	PendingOwner sdk.AccAddress `json:"pending_owner"`
}

// XRPLToken is XRPL token representation on coreum.
type XRPLToken struct {
	Issuer      string `json:"issuer"`
	Currency    string `json:"currency"`
	CoreumDenom string `json:"coreum_denom"`
}

// CoreumToken is coreum token registered on the contract.
//
//nolint:revive //kept for the better naming convention.
type CoreumToken struct {
	Denom        string `json:"denom"`
	Decimals     uint32 `json:"decimals"`
	XRPLCurrency string `json:"xrpl_currency"`
}

// ******************** Internal transport object  ********************

type instantiateRequest struct {
	Owner                sdk.AccAddress   `json:"owner"`
	Relayers             []sdk.AccAddress `json:"relayers"`
	EvidenceThreshold    int              `json:"evidence_threshold"`
	UsedTicketsThreshold int              `json:"used_tickets_threshold"`
}

type transferOwnershipRequest struct {
	TransferOwnership struct {
		NewOwner sdk.AccAddress `json:"new_owner"`
	} `json:"transfer_ownership"`
}

type registerCoreumTokenRequest struct {
	Denom    string `json:"denom"`
	Decimals uint32 `json:"decimals"`
}

type registerXRPLTokenRequest struct {
	Issuer   string `json:"issuer"`
	Currency string `json:"currency"`
}

type xrplTokensResponse struct {
	Tokens []XRPLToken `json:"tokens"`
}

type coreumTokensResponse struct {
	Tokens []CoreumToken `json:"tokens"`
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
		ContractAddress: contractAddress,
		GasAdjustment:   1.3,
		// 1.2
		GasPriceAdjustment: sdk.NewDecFromInt(sdk.NewInt(12)).QuoInt64(10),
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
	}
}

// DeployAndInstantiate instantiates the contract.
func (c *ContractClient) DeployAndInstantiate(ctx context.Context, sender sdk.AccAddress, byteCode []byte, config InstantiationConfig) (sdk.AccAddress, error) {
	msgStoreCode := &wasmtypes.MsgStoreCode{
		Sender:       sender.String(),
		WASMByteCode: byteCode,
	}
	c.log.Info("Deploying contract bytecode.")

	res, err := client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msgStoreCode)
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy wasm bytecode")
	}
	codeID, err := event.FindUint64EventAttribute(res.Events, wasmtypes.EventTypeStoreCode, wasmtypes.AttributeKeyCodeID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find code ID in the tx result")
	}
	c.log.Info("The contract bytecode is deployed.", logger.Uint64Filed("codeID", codeID))

	reqPayload, err := json.Marshal(instantiateRequest{
		Owner:                config.Owner,
		Relayers:             config.Relayers,
		EvidenceThreshold:    config.EvidenceThreshold,
		UsedTicketsThreshold: config.UsedTicketsThreshold,
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

	c.log.Info("Instantiating contract.", logger.AnyFiled("msg", msg))
	res, err = client.BroadcastTx(ctx, c.clientCtx.WithFromAddress(sender), c.getTxFactory(), msg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to deploy bytecode")
	}

	contractAddr, err := event.FindStringEventAttribute(res.Events, wasmtypes.EventTypeInstantiate, wasmtypes.AttributeKeyContractAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find contract address in the tx result")
	}

	sdkContractAddr, err := sdk.AccAddressFromBech32(contractAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert contract address to sdk.AccAddress")
	}
	c.log.Info("The contract is instantiated.", logger.StringFiled("address", sdkContractAddr.String()))

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

// ******************** Execute ********************

// TransferOwnership executes `update_ownership` method with transfer action.
func (c *ContractClient) TransferOwnership(ctx context.Context, sender, newOwner sdk.AccAddress) (*sdk.TxResponse, error) {
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

// RegisterCoreumToken executes `register_coreum_token` method.
func (c *ContractClient) RegisterCoreumToken(ctx context.Context, sender sdk.AccAddress, denom string, decimals uint32) (*sdk.TxResponse, error) {
	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]registerCoreumTokenRequest{
			ExecMethodRegisterCoreumToken: {
				Denom:    denom,
				Decimals: decimals,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

// RegisterXRPLToken executes `register_xrpl_token` method.
func (c *ContractClient) RegisterXRPLToken(ctx context.Context, sender sdk.AccAddress, issuer, currency string) (*sdk.TxResponse, error) {
	fee, err := c.queryAssetFTIssueFee(ctx)
	if err != nil {
		return nil, err
	}

	txRes, err := c.execute(ctx, sender, execRequest{
		Body: map[ExecMethod]registerXRPLTokenRequest{
			ExecMethodRegisterXRPLToken: {
				Issuer:   issuer,
				Currency: currency,
			},
		},
		Funds: sdk.NewCoins(fee),
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

// GetXRPLTokens returns a list of paginated XRPL tokens.
func (c *ContractClient) getPaginatedXRPLTokens(ctx context.Context, offset *uint64, limit *uint32) ([]XRPLToken, error) {
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

// GetXRPLTokens returns a list of paginated XRPL tokens.
func (c *ContractClient) getPaginatedCoreumTokens(ctx context.Context, offset *uint64, limit *uint32) ([]CoreumToken, error) {
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

func (c *ContractClient) execute(ctx context.Context, sender sdk.AccAddress, requests ...execRequest) (*sdk.TxResponse, error) {
	if c.cfg.ContractAddress == nil {
		return nil, errors.New("failed to execute with empty contract address")
	}

	msgs := make([]sdk.Msg, 0, len(requests))
	for _, req := range requests {
		payload, err := json.Marshal(req.Body)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to marshal payload, requiest:%v", req.Body)
		}
		c.log.Debug("Executing contract", logger.StringFiled("payload", string(payload)))
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
		return nil, errors.Wrapf(err, "failed to execute transaction, requiests:%v", requests)
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
	c.log.Debug("Querying contract", logger.StringFiled("payload", string(payload)))

	query := &wasmtypes.QuerySmartContractStateRequest{
		Address:   c.cfg.ContractAddress.String(),
		QueryData: payload,
	}
	resp, err := c.wasmClient.SmartContractState(ctx, query)
	if err != nil {
		return errors.Wrapf(err, "query failed, requiest:%v", request)
	}

	c.log.Debug("Query is succeeded", logger.StringFiled("data", string(resp.Data)))
	if err := json.Unmarshal(resp.Data, response); err != nil {
		return errors.Wrapf(err, "failed to unmarshal wasm contract response, requiest:%v, response:%s", request, string(resp.Data))
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

// ******************** Error func ********************

// IsNotOwnerError returns true if error is `not owner` error.
func IsNotOwnerError(err error) bool {
	return isError(err, notOwnerErrorString)
}

// IsCoreumTokenAlreadyRegisteredError returns true if error is `CoreumTokenAlreadyRegistered` error.
func IsCoreumTokenAlreadyRegisteredError(err error) bool {
	return isError(err, coreumTokenAlreadyRegisteredErrorString)
}

// IsXRPLTokenAlreadyRegisteredError returns true if error is `XRPLTokenAlreadyRegistered` error.
func IsXRPLTokenAlreadyRegisteredError(err error) bool {
	return isError(err, xrplTokenAlreadyRegisteredErrorString)
}

func isError(err error, errorString string) bool {
	return err != nil && strings.Contains(err.Error(), errorString)
}
