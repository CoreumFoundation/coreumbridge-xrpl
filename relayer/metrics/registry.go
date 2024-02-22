package metrics

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	relayerErrorsTotalMetricName             = "relayer_errors_total"
	xrplChainBaseFeeMetricName               = "xrpl_chain_base_fee"
	contractConfigXRPLBaseFeeMetricName      = "contract_config_xrpl_base_fee"
	xrplBridgeAccountBalancesMetricName      = "xrpl_bridge_account_balances"
	contractBalancesBalancesMetricName       = "contract_balances"
	pendingOperationsMetricName              = "pending_operations"
	transactionEvidencesMetricName           = "transaction_evidences"
	relayerBalancesMetricName                = "relayer_balances"
	currentRecentScanXRPLedgerMetricName     = "current_recent_scan_xrpl_ledger"
	currentHistoricalScanXRPLedgerMetricName = "current_historical_scan_xrpl_ledger"
	freeContractTicketsMetricName            = "free_contract_tickets"
	freeXRPLTicketsMetricName                = "free_xrpl_tickets"

	// XRPLCurrencyIssuerLabel is XRPL currency issuer label.
	XRPLCurrencyIssuerLabel = "xrpl_currency_issuer"
	// CoreumDenomLabel is Coreum denom label.
	CoreumDenomLabel = "coreum_denom"
	// OperationIDLabel is operation ID label.
	OperationIDLabel = "operation_id"
	// EvidenceHashLabel is evidence hash label.
	EvidenceHashLabel = "evidence_hash"
	// AddressLabel is address label.
	AddressLabel = "address"
)

// Registry contains metrics.
type Registry struct {
	ErrorCounter                        prometheus.Counter
	XRPLChainBaseFeeGauge               prometheus.Gauge
	ContractConfigXRPLBaseFeeGauge      prometheus.Gauge
	XRPLBridgeAccountBalancesGaugeVec   *prometheus.GaugeVec
	ContractBalancesGaugeVec            *prometheus.GaugeVec
	PendingOperationsGaugeVec           *prometheus.GaugeVec
	TransactionEvidencesGaugeVec        *prometheus.GaugeVec
	RelayerBalancesGaugeVec             *prometheus.GaugeVec
	CurrentRecentScanXRPLedgerGauge     prometheus.Gauge
	CurrentHistoricalScanXRPLedgerGauge prometheus.Gauge
	FreeContractTicketsGauge            prometheus.Gauge
	FreeXRPLTicketsGauge                prometheus.Gauge
}

// NewRegistry returns new metric registry.
func NewRegistry() *Registry {
	return &Registry{
		ErrorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: relayerErrorsTotalMetricName,
			Help: "Error counter",
		}),
		XRPLChainBaseFeeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: xrplChainBaseFeeMetricName,
			Help: "Base transaction fee on the XRPL chain",
		}),
		ContractConfigXRPLBaseFeeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: contractConfigXRPLBaseFeeMetricName,
			Help: "Contract config XRPL base fee",
		}),
		XRPLBridgeAccountBalancesGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: xrplBridgeAccountBalancesMetricName,
			Help: "XRPL bridge account balances",
		},
			[]string{XRPLCurrencyIssuerLabel},
		),
		ContractBalancesGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: contractBalancesBalancesMetricName,
			Help: "Contract balances",
		},
			[]string{
				XRPLCurrencyIssuerLabel,
				CoreumDenomLabel,
			},
		),
		PendingOperationsGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: pendingOperationsMetricName,
			Help: "Pending operations",
		},
			[]string{
				OperationIDLabel,
			},
		),
		TransactionEvidencesGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: transactionEvidencesMetricName,
			Help: "Transaction evidences",
		},
			[]string{
				EvidenceHashLabel,
			},
		),
		RelayerBalancesGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: relayerBalancesMetricName,
			Help: "Relayer evidences",
		},
			[]string{
				AddressLabel,
			},
		),
		CurrentRecentScanXRPLedgerGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: currentRecentScanXRPLedgerMetricName,
			Help: "Current recent scan XRPL ledger",
		}),
		CurrentHistoricalScanXRPLedgerGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: currentHistoricalScanXRPLedgerMetricName,
			Help: "Current historical scan XRPL ledger",
		}),
		FreeContractTicketsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: freeContractTicketsMetricName,
			Help: "Free contract tickets",
		}),
		FreeXRPLTicketsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: freeXRPLTicketsMetricName,
			Help: "Free XRPL tickets",
		}),
	}
}

// Register registers all the metrics to prometheus.
func (m *Registry) Register(registry prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.ErrorCounter,
		m.XRPLChainBaseFeeGauge,
		m.ContractConfigXRPLBaseFeeGauge,
		m.XRPLBridgeAccountBalancesGaugeVec,
		m.ContractBalancesGaugeVec,
		m.PendingOperationsGaugeVec,
		m.TransactionEvidencesGaugeVec,
		m.RelayerBalancesGaugeVec,
		m.CurrentRecentScanXRPLedgerGauge,
		m.CurrentHistoricalScanXRPLedgerGauge,
		m.FreeContractTicketsGauge,
		m.FreeXRPLTicketsGauge,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return errors.Wrapf(err, "failed to register metric collector")
		}
	}

	return nil
}
