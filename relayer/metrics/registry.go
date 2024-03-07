package metrics

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	relayerErrorsTotalMetricName                      = "relayer_errors_total"
	xrplChainBaseFeeMetricName                        = "xrpl_chain_base_fee"
	contractConfigXRPLBaseFeeMetricName               = "contract_config_xrpl_base_fee"
	xrplBridgeAccountBalancesMetricName               = "xrpl_bridge_account_balances"
	contractBalancesBalancesMetricName                = "contract_balances"
	pendingOperationsMetricName                       = "pending_operations"
	transactionEvidencesMetricName                    = "transaction_evidences"
	relayerBalancesMetricName                         = "relayer_balances"
	xrplAccountRecentHistoryScanLedgerIndexMetricName = "xrpl_account_recent_history_scan_ledger_index"
	xrplAccountFullHistoryScanLedgerIndexMetricName   = "xrpl_account_full_history_scan_ledger_index"
	freeContractTicketsMetricName                     = "free_contract_tickets"
	freeXRPLTicketsMetricName                         = "free_xrpl_tickets"
	bridgeStateMetricName                             = "bridge_state"
	maliciousBehaviourMetricName                      = "malicious_behaviour"
	relayerActivityMetricName                         = "relayer_activity"
	xrplTokensCoreumSupplyMetricName                  = "xrpl_tokens_coreum_supply"
	xrplBridgeAccountReservesMetricName               = "xrpl_bridge_account_reserves"

	// XRPLCurrencyIssuerLabel is XRPL currency issuer label.
	XRPLCurrencyIssuerLabel = "xrpl_currency_issuer"
	// CoreumDenomLabel is Coreum denom label.
	CoreumDenomLabel = "coreum_denom"
	// OperationIDLabel is operation ID label.
	OperationIDLabel = "operation_id"
	// EvidenceHashLabel is evidence hash label.
	EvidenceHashLabel = "evidence_hash"
	// RelayerCoremAddressLabel is address label.
	RelayerCoremAddressLabel = "relayer_coreum_address"
	// MaliciousBehaviourKeyLabel malicious behaviour key label.
	MaliciousBehaviourKeyLabel = "malicious_behaviour_key"
	// ContractActionLabel is contract action label.
	ContractActionLabel = "action"
)

// Registry contains metrics.
type Registry struct {
	RelayerErrorCounter                          prometheus.Counter
	XRPLChainBaseFeeGauge                        prometheus.Gauge
	ContractConfigXRPLBaseFeeGauge               prometheus.Gauge
	XRPLBridgeAccountBalancesGaugeVec            *prometheus.GaugeVec
	ContractBalancesGaugeVec                     *prometheus.GaugeVec
	PendingOperationsGaugeVec                    *prometheus.GaugeVec
	TransactionEvidencesGaugeVec                 *prometheus.GaugeVec
	RelayerBalancesGaugeVec                      *prometheus.GaugeVec
	XRPLAccountRecentHistoryScanLedgerIndexGauge prometheus.Gauge
	XRPLAccountFullHistoryScanLedgerIndexGauge   prometheus.Gauge
	FreeContractTicketsGauge                     prometheus.Gauge
	FreeXRPLTicketsGauge                         prometheus.Gauge
	BridgeStateGauge                             prometheus.Gauge
	MaliciousBehaviourGaugeVec                   *prometheus.GaugeVec
	RelayerActivityGaugeVec                      *prometheus.GaugeVec
	XRPLTokensCoreumSupplyGaugeVec               *prometheus.GaugeVec
	XRPLBridgeAccountReservesGauge               prometheus.Gauge
}

// NewRegistry returns new metric registry.
//
//nolint:funlen // linear objects initialisation
func NewRegistry() *Registry {
	return &Registry{
		RelayerErrorCounter: prometheus.NewCounter(prometheus.CounterOpts{
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
				RelayerCoremAddressLabel,
			},
		),
		XRPLAccountRecentHistoryScanLedgerIndexGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: xrplAccountRecentHistoryScanLedgerIndexMetricName,
			Help: "XRPL account recent history scan ledger index",
		}),
		XRPLAccountFullHistoryScanLedgerIndexGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: xrplAccountFullHistoryScanLedgerIndexMetricName,
			Help: "XRPL account full history scan ledger index",
		}),
		FreeContractTicketsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: freeContractTicketsMetricName,
			Help: "Free contract tickets",
		}),
		FreeXRPLTicketsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: freeXRPLTicketsMetricName,
			Help: "Free XRPL tickets",
		}),
		BridgeStateGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: bridgeStateMetricName,
			Help: "Bridge state",
		}),
		MaliciousBehaviourGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: maliciousBehaviourMetricName,
			Help: "Malicious behaviour",
		},
			[]string{
				MaliciousBehaviourKeyLabel,
			},
		),
		RelayerActivityGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: relayerActivityMetricName,
			Help: "Relayer activity",
		},
			[]string{
				RelayerCoremAddressLabel,
				ContractActionLabel,
			},
		),
		XRPLTokensCoreumSupplyGaugeVec: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: xrplTokensCoreumSupplyMetricName,
			Help: "XRPL tokens supply on Coreum",
		},
			[]string{
				XRPLCurrencyIssuerLabel,
				CoreumDenomLabel,
			},
		),
		XRPLBridgeAccountReservesGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: xrplBridgeAccountReservesMetricName,
			Help: "XRPL bridge account reserves",
		}),
	}
}

// Register registers all the metrics to prometheus.
func (m *Registry) Register(registry prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.RelayerErrorCounter,
		m.XRPLChainBaseFeeGauge,
		m.ContractConfigXRPLBaseFeeGauge,
		m.XRPLBridgeAccountBalancesGaugeVec,
		m.ContractBalancesGaugeVec,
		m.PendingOperationsGaugeVec,
		m.TransactionEvidencesGaugeVec,
		m.RelayerBalancesGaugeVec,
		m.XRPLAccountRecentHistoryScanLedgerIndexGauge,
		m.XRPLAccountFullHistoryScanLedgerIndexGauge,
		m.FreeContractTicketsGauge,
		m.FreeXRPLTicketsGauge,
		m.BridgeStateGauge,
		m.MaliciousBehaviourGaugeVec,
		m.RelayerActivityGaugeVec,
		m.XRPLTokensCoreumSupplyGaugeVec,
		m.XRPLBridgeAccountReservesGauge,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return errors.Wrapf(err, "failed to register metric collector")
		}
	}

	return nil
}

// IncrementRelayerErrorCounter increments RelayerErrorCounter.
func (m *Registry) IncrementRelayerErrorCounter() {
	m.RelayerErrorCounter.Inc()
}

// SetXRPLAccountRecentHistoryScanLedgerIndex sets XRPLAccountRecentHistoryScanLedgerIndexGauge value.
func (m *Registry) SetXRPLAccountRecentHistoryScanLedgerIndex(index float64) {
	m.XRPLAccountRecentHistoryScanLedgerIndexGauge.Set(index)
}

// SetXRPLAccountFullHistoryScanLedgerIndex sets XRPLAccountFullHistoryScanLedgerIndexGauge value.
func (m *Registry) SetXRPLAccountFullHistoryScanLedgerIndex(index float64) {
	m.XRPLAccountFullHistoryScanLedgerIndexGauge.Set(index)
}

// SetMaliciousBehaviourKey sets the MaliciousBehaviourGaugeVec value to 1 with MaliciousBehaviourKeyLabel and
// provided key.
func (m *Registry) SetMaliciousBehaviourKey(key string) {
	m.MaliciousBehaviourGaugeVec.WithLabelValues(key).Set(1)
}
