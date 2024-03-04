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
	"github.com/prometheus/client_golang/prometheus"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	feemodeltypes "github.com/CoreumFoundation/coreum/v4/x/feemodel/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// XRPLRPCClient is XRPL RPC client interface.
type XRPLRPCClient interface {
	ServerState(ctx context.Context) (xrpl.ServerStateResult, error)
	GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error)
	AccountInfo(ctx context.Context, acc rippledata.Account) (xrpl.AccountInfoResult, error)
}

// ContractClient is the interface for the contract client.
type ContractClient interface {
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
	GetCoreumTokens(ctx context.Context) ([]coreum.CoreumToken, error)
	GetXRPLTokens(ctx context.Context) ([]coreum.XRPLToken, error)
	GetContractAddress() sdk.AccAddress
	GetPendingOperations(ctx context.Context) ([]coreum.Operation, error)
	GetTransactionEvidences(ctx context.Context) ([]coreum.TransactionEvidence, error)
	GetAvailableTickets(ctx context.Context) ([]uint32, error)
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
	feemodelClient   feemodeltypes.QueryClient
	coreumBankClient banktypes.QueryClient

	pendingOperationsCachedKeys    map[string]struct{}
	transactionEvidencesCachedKeys map[string]struct{}
	relayersBalancesCachedKeys     map[string]struct{}
	cacheMu                        sync.Mutex
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
		feemodelClient:   feemodeltypes.NewQueryClient(clientContext),
		coreumBankClient: banktypes.NewQueryClient(clientContext),

		pendingOperationsCachedKeys:    make(map[string]struct{}),
		transactionEvidencesCachedKeys: make(map[string]struct{}),
		relayersBalancesCachedKeys:     make(map[string]struct{}),
		cacheMu:                        sync.Mutex{},
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
		transactionEvidencesMetricName:      c.collectTransactionEvidences,
		relayerBalancesMetricName:           c.collectRelayerBalances,
		fmt.Sprintf("%s/%s", freeContractTicketsMetricName, freeXRPLTicketsMetricName): c.collectFreeTickets,
		bridgeStateMetricName: c.collectBridgeState,
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
	c.registry.XRPLChainBaseFeeGauge.Set(float64(xrplChainBaseFee))

	return nil
}

func (c *PeriodicCollector) collectContractConfigXRPLBaseFee(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}
	c.registry.ContractConfigXRPLBaseFeeGauge.Set(float64(contractCfg.XRPLBaseFee))

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
		c.registry.XRPLBridgeAccountBalancesGaugeVec.
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

	denomToXRPLCurrencyIssuer := make(map[string]string)
	denomToDecimals := make(map[string]uint32)

	coreumTokens, err := c.contractClient.GetCoreumTokens(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get registered Coreum tokens")
	}

	for _, token := range coreumTokens {
		denomToXRPLCurrencyIssuer[token.Denom] = fmt.Sprintf("%s/%s", token.XRPLCurrency, contractCfg.BridgeXRPLAddress)
		denomToDecimals[token.Denom] = token.Decimals
	}

	xrplTokens, err := c.contractClient.GetXRPLTokens(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get registered XRPL tokens")
	}

	for _, token := range xrplTokens {
		denomToXRPLCurrencyIssuer[token.CoreumDenom] = fmt.Sprintf("%s/%s", token.Currency, token.Issuer)
		if token.Currency == xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency) &&
			token.Issuer == xrpl.XRPTokenIssuer.String() {
			denomToDecimals[token.CoreumDenom] = xrpl.XRPCurrencyDecimals
			continue
		}
		denomToDecimals[token.CoreumDenom] = xrpl.XRPLIssuedTokenDecimals
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
		c.registry.ContractBalancesGaugeVec.
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

	currentValues := lo.SliceToMap(pendingOperations, func(operation coreum.Operation) (string, float64) {
		operationID := operation.GetOperationID()
		return strconv.Itoa(int(operationID)), float64(len(operation.Signatures))
	})
	c.updateGaugeVecAndCachedValues(currentValues, c.pendingOperationsCachedKeys, c.registry.PendingOperationsGaugeVec)

	return nil
}

func (c *PeriodicCollector) collectTransactionEvidences(ctx context.Context) error {
	transactionEvidences, err := c.contractClient.GetTransactionEvidences(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get transaction evidences")
	}
	currentValues := lo.SliceToMap(transactionEvidences, func(evidences coreum.TransactionEvidence) (string, float64) {
		return evidences.Hash, float64(len(evidences.RelayerAddresses))
	})
	c.updateGaugeVecAndCachedValues(
		currentValues, c.transactionEvidencesCachedKeys, c.registry.TransactionEvidencesGaugeVec,
	)

	return nil
}

func (c *PeriodicCollector) collectRelayerBalances(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}

	// take the denom from gas price
	gasPrice, err := c.feemodelClient.MinGasPrice(ctx, &feemodeltypes.QueryMinGasPriceRequest{})
	if err != nil {
		return errors.Wrap(err, "failed to get gas price")
	}

	currentValues := make(map[string]float64, 0)
	mu := sync.Mutex{}
	if err := parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for _, relayer := range contractCfg.Relayers {
			relayerAddress := relayer.CoreumAddress.String()
			spawn(
				fmt.Sprintf("get-relayer-balance-%s", relayerAddress),
				parallel.Continue, func(ctx context.Context) error {
					balancesRes, err := c.coreumBankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
						Address: relayerAddress,
						Denom:   gasPrice.MinGasPrice.Denom,
					})
					if err != nil {
						return errors.Wrapf(err, "failed to get relayer %s balance", relayerAddress)
					}
					mu.Lock()
					defer mu.Unlock()
					if balancesRes.Balance == nil {
						currentValues[relayerAddress] = 0
						return nil
					}
					currentValues[relayerAddress] = c.truncateFloatByTruncationPrecision(
						truncateAmountWithDecimals(coreum.TokenDecimals, balancesRes.Balance.Amount),
					)
					return nil
				})
		}
		return nil
	}); err != nil {
		return errors.Wrap(err, "failed to get relayer currentValues")
	}
	c.updateGaugeVecAndCachedValues(currentValues, c.relayersBalancesCachedKeys, c.registry.RelayerBalancesGaugeVec)

	return nil
}

func (c *PeriodicCollector) collectFreeTickets(ctx context.Context) error {
	availableContractTickets, err := c.contractClient.GetAvailableTickets(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get available contract tickets")
	}

	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}

	xrplBridgeAccount, err := rippledata.NewAccountFromAddress(contractCfg.BridgeXRPLAddress)
	if err != nil {
		return errors.Wrapf(
			err,
			"failed to convert bridge XRPL address to rippledata.Account, address:%s",
			contractCfg.BridgeXRPLAddress,
		)
	}

	accountInfo, err := c.xrplRPCClient.AccountInfo(ctx, *xrplBridgeAccount)
	if err != nil {
		return errors.Wrap(err, "failed to get XRPL bridge account info")
	}

	c.registry.FreeContractTicketsGauge.Set(float64(len(availableContractTickets)))
	var xrplTicketCount float64
	if accountInfo.AccountData.TicketCount != nil {
		xrplTicketCount = float64(*accountInfo.AccountData.TicketCount)
	}
	c.registry.FreeXRPLTicketsGauge.Set(xrplTicketCount)

	return nil
}

func (c *PeriodicCollector) collectBridgeState(ctx context.Context) error {
	contractCfg, err := c.contractClient.GetContractConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get contract config")
	}

	switch contractCfg.BridgeState {
	case coreum.BridgeStateHalted:
		c.registry.BridgeStateGauge.Set(0)
	case coreum.BridgeStateActive:
		c.registry.BridgeStateGauge.Set(1)
	default:
		return errors.Wrapf(err, "received unexpected bridge state:%s", contractCfg.BridgeState)
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

func (c *PeriodicCollector) updateGaugeVecAndCachedValues(
	currentValues map[string]float64,
	cachedKeys map[string]struct{},
	gaugeVec *prometheus.GaugeVec,
) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	// delete removed keys
	for k := range cachedKeys {
		if _, ok := currentValues[k]; !ok {
			delete(cachedKeys, k)
			gaugeVec.DeleteLabelValues(k)
		}
	}
	for k, v := range currentValues {
		gaugeVec.WithLabelValues(k).Set(v)
		cachedKeys[k] = struct{}{}
	}
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
