package metrics

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	relayerErrorsTotalMetricName        = "relayer_errors_total"
	xrplChainBaseFeeMetricName          = "xrpl_chain_base_fee"
	contractConfigXRPLBaseFeeMetricName = "contract_config_xrpl_base_fee"
	xrplBridgeAccountBalancesMetricName = "xrpl_bridge_account_balances"
	contractBalancesBalancesMetricName  = "contract_balances"
	pendingOperationsMetricName         = "pending_operations"

	// XRPLCurrencyIssuerLabel is XRPL currency issuer label.
	XRPLCurrencyIssuerLabel = "xrpl_currency_issuer"
	// CoreumDenomLabel is Coreum denom label.
	CoreumDenomLabel = "coreum_denom"
	// OperationIDLabel is operation ID label.
	OperationIDLabel = "operation_id"
)

// Registry contains metrics.
type Registry struct {
	ErrorCounter              prometheus.Counter
	XRPLChainBaseFee          prometheus.Gauge
	ContractConfigXRPLBaseFee prometheus.Gauge
	XRPLBridgeAccountBalances *prometheus.GaugeVec
	ContractBalances          *prometheus.GaugeVec
	PendingOperations         *prometheus.GaugeVec
}

// NewRegistry returns new metric registry.
func NewRegistry() *Registry {
	return &Registry{
		ErrorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: relayerErrorsTotalMetricName,
			Help: "Error counter",
		}),
		XRPLChainBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: xrplChainBaseFeeMetricName,
			Help: "Base transaction fee on the XRPL chain",
		}),
		ContractConfigXRPLBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: contractConfigXRPLBaseFeeMetricName,
			Help: "Contract config XRPL base fee",
		}),
		XRPLBridgeAccountBalances: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: xrplBridgeAccountBalancesMetricName,
			Help: "XRPL bridge account balances",
		},
			[]string{XRPLCurrencyIssuerLabel},
		),
		ContractBalances: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: contractBalancesBalancesMetricName,
			Help: "Contract balances",
		},
			[]string{
				XRPLCurrencyIssuerLabel,
				CoreumDenomLabel,
			},
		),
		PendingOperations: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: pendingOperationsMetricName,
			Help: "Pending operations",
		},
			[]string{
				OperationIDLabel,
			},
		),
	}
}

// Register registers all the metrics to prometheus.
func (m *Registry) Register(registry prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.ErrorCounter,
		m.XRPLChainBaseFee,
		m.ContractConfigXRPLBaseFee,
		m.XRPLBridgeAccountBalances,
		m.ContractBalances,
		m.PendingOperations,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return errors.Wrapf(err, "failed to register metric collector")
		}
	}

	return nil
}
