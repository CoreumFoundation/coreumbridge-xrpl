package metrics

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	ServerState(ctx context.Context) (xrpl.ServerStateResult, error)
	GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error)
}

// ContractClient is the interface for the contract client.
type ContractClient interface {
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
	GetCoreumTokens(ctx context.Context) ([]coreum.CoreumToken, error)
	GetXRPLTokens(ctx context.Context) ([]coreum.XRPLToken, error)
	GetContractAddress() sdk.AccAddress
	GetPendingOperations(ctx context.Context) ([]coreum.Operation, error)
}

// PeriodicCollectorConfig is PeriodicCollector config.
type PeriodicCollectorConfig struct {
	RepeatDelay time.Duration
	// how many decimals we keep in the float values
	FloatTruncationPrecision uint32
}

// DefaultPeriodicCollectorConfig returns default PeriodicCollectorConfig.
func DefaultPeriodicCollectorConfig() PeriodicCollectorConfig {
	return PeriodicCollectorConfig{
		RepeatDelay:              30 * time.Second,
		FloatTruncationPrecision: 2,
	}
}

// PeriodicCollector is metric periodic scanner responsible for the periodic collecting of the metrics.
type PeriodicCollector struct {
	cfg              PeriodicCollectorConfig
	log              logger.Logger
	registry         *Registry
	xrplRPCClient    XRPLRPCClient
	contractClient   ContractClient
	coreumBankClient banktypes.QueryClient

	pendingOperationsCache   map[uint32]struct{}
	pendingOperationsCacheMu sync.Mutex
}

// NewPeriodicCollector returns a new instance of the PeriodicCollector.
func NewPeriodicCollector(
	cfg PeriodicCollectorConfig,
	log logger.Logger,
	registry *Registry,
	xrplRPCClient XRPLRPCClient,
	contractClient ContractClient,
	clientContext client.Context,
) *PeriodicCollector {
	return &PeriodicCollector{
		cfg:              cfg,
		log:              log,
		registry:         registry,
		xrplRPCClient:    xrplRPCClient,
		contractClient:   contractClient,
		coreumBankClient: banktypes.NewQueryClient(clientContext),

		pendingOperationsCacheMu: sync.Mutex{},
		pendingOperationsCache:   make(map[uint32]struct{}),
	}
}

// Start starts the periodic collector.
func (c *PeriodicCollector) Start(ctx context.Context) error {
	periodicCollectors := map[string]func(ctx context.Context) error{
		xrplChainBaseFeeMetricName:          c.collectXRPLChainBaseFee,
		contractConfigXRPLBaseFeeMetricName: c.collectContractConfigXRPLBaseFee,
		xrplBridgeAccountBalancesMetricName: c.collectXRPLBridgeAccountBalances,
		contractBalancesBalancesMetricName:  c.collectContractBalances,
		pendingOperationsMetricName:         c.collectPendingOperations,
	}
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for name, collector := range periodicCollectors {
			// copy to use in spawn
			nameCopy := name
			collectorCopy := collector
			spawn(nameCopy, parallel.Continue, func(ctx context.Context) error {
				return c.collectWithRepeat(ctx, nameCopy, func() {
					if err := collectorCopy(ctx); err != nil {
						c.log.Error(
							ctx,
							"failed to collect metric",
							zap.String("name", nameCopy),
							zap.Error(err),
						)
					}
				})
			})
		}
		return nil
	})
}

func (c *PeriodicCollector) collectXRPLChainBaseFee(ctx context.Context) error {
	serverState, err := c.xrplRPCClient.ServerState(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get XRPL server state")
	}
	xrplChainBaseFee := xrpl.ComputeXRPLBaseFee(
		serverState.State.ValidatedLedger.BaseFee,
		serverState.State.LoadFactor,
		serverState.State.LoadBase,
	)
	c.registry.XRPLChainBaseFee.Set(float64(xrplChainBaseFee))

	return nil
}

func (c *PeriodicCollector) collectContractConfigXRPLBaseFee(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}
	c.registry.ContractConfigXRPLBaseFee.Set(float64(contractCfg.XRPLBaseFee))

	return nil
}

func (c *PeriodicCollector) collectXRPLBridgeAccountBalances(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}
	xrplBridgeAccount, err := rippledata.NewAccountFromAddress(contractCfg.BridgeXRPLAddress)
	if err != nil {
		return errors.Wrapf(
			err,
			"faild to convert bridge XRPL address to rippledata.Account, bridge XRPL address:%s",
			contractCfg.BridgeXRPLAddress,
		)
	}

	xrplBalances, err := c.xrplRPCClient.GetXRPLBalances(ctx, *xrplBridgeAccount)
	if err != nil {
		return errors.Wrap(err, "failed to get XRPL bridge account balances")
	}

	for _, balance := range xrplBalances {
		floatValue := c.truncateFloatByTruncationPrecision(balance.Float())
		c.registry.XRPLBridgeAccountBalances.
			WithLabelValues(fmt.Sprintf("%s/%s", xrpl.ConvertCurrencyToString(balance.Currency), balance.Issuer.String())).
			Set(floatValue)
	}

	return nil
}

func (c *PeriodicCollector) collectContractBalances(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}

	coreumTokens, err := c.contractClient.GetCoreumTokens(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get registered Coreum tokens")
	}

	denomToXRPLCurrencyIssuer := make(map[string]string)
	denomToDecimals := make(map[string]uint32)

	for _, token := range coreumTokens {
		denomToXRPLCurrencyIssuer[token.Denom] = fmt.Sprintf("%s/%s", token.XRPLCurrency, contractCfg.BridgeXRPLAddress)
		denomToDecimals[token.Denom] = token.Decimals
	}

	contractBalancesRes, err := c.coreumBankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
		Address:    c.contractClient.GetContractAddress().String(),
		Pagination: &query.PageRequest{Limit: query.MaxLimit},
	})
	if err != nil {
		return errors.Wrap(err, "failed to contract Coreum balances")
	}

	for _, balance := range contractBalancesRes.Balances {
		denom := balance.Denom
		xrplCurrencyIssuerLabel, ok := denomToXRPLCurrencyIssuer[denom]
		if !ok {
			continue
		}
		decimals, ok := denomToDecimals[denom]
		if !ok {
			continue
		}
		c.registry.ContractBalances.
			WithLabelValues(xrplCurrencyIssuerLabel, denom).
			Set(c.truncateFloatByTruncationPrecision(truncateAmountWithDecimals(decimals, balance.Amount)))
	}

	return nil
}

func (c *PeriodicCollector) collectPendingOperations(ctx context.Context) error {
	pendingOperations, err := c.contractClient.GetPendingOperations(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get pending operations")
	}
	c.pendingOperationsCacheMu.Lock()
	defer c.pendingOperationsCacheMu.Unlock()

	currentPendingOperations := make(map[uint32]struct{})
	for _, operation := range pendingOperations {
		operationID := operation.GetOperationID()
		// save operation ID as label and signatures len as value
		c.registry.PendingOperations.WithLabelValues(strconv.Itoa(int(operationID))).
			Set(float64(len(operation.Signatures)))
		currentPendingOperations[operationID] = struct{}{}
		c.pendingOperationsCache[operationID] = struct{}{}
	}
	// delete finished operations
	for operationID := range c.pendingOperationsCache {
		if _, ok := currentPendingOperations[operationID]; !ok {
			// the operation was removed
			delete(c.pendingOperationsCache, operationID)
			c.registry.PendingOperations.DeleteLabelValues(strconv.Itoa(int(operationID)))
		}
	}

	return nil
}

func truncateAmountWithDecimals(decimals uint32, amount sdkmath.Int) float64 {
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	balanceRat := big.NewRat(0, 1).SetFrac(amount.BigInt(), tenPowerDec)
	// float64 should cover the range with enough precision
	floatValue, _ := balanceRat.Float64()
	return floatValue
}

func (c *PeriodicCollector) truncateFloatByTruncationPrecision(val float64) float64 {
	ratio := math.Pow(10, float64(c.cfg.FloatTruncationPrecision))
	return math.Trunc(val*ratio) / ratio
}

func (c *PeriodicCollector) collectWithRepeat(ctx context.Context, name string, collector func()) error {
	c.log.Info(ctx,
		"Starting collecting of the metric.",
		zap.String("metricName", name),
	)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			collector()
			c.log.Debug(ctx,
				"Waiting before the repeat of the metric collecting.",
				zap.String("metricName", name),
				zap.String("delay", c.cfg.RepeatDelay.String()),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.cfg.RepeatDelay):
			}
		}
	}
}
