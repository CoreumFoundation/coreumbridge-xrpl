package xrpl

import (
	rippledata "github.com/rubblelabs/ripple/data"
)

// Error codes.
const (
	// TecPathDryTxResult defines that provided paths did not have enough liquidity to send anything at all.
	//	This could mean that the source and destination accounts are not linked by trust lines.
	TecPathDryTxResult = "tecPATH_DRY"
	// TecPathPartialTxResult defines that transaction failed because the provided paths did not have enough liquidity
	//	to send the full amount.
	TecPathPartialTxResult = "tecPATH_PARTIAL"
	// TecNoDstTxResult defines that provided the account on the receiving end of the transaction does not exist.
	// This includes Payment and TrustSet transaction types. (It could be created if it received enough XRP.)
	TecNoDstTxResult = "tecNO_DST"
	// TefNOTicketTxResult defines the result which indicates the usage of the passed ticket or not created ticket.
	TefNOTicketTxResult = "tefNO_TICKET"
	// TefPastSeqTxResult defines that the usage of the sequence in the past.
	TefPastSeqTxResult = "tefPAST_SEQ"
	// TefMaxLedgerTxResult defines that ledger sequence too high.
	TefMaxLedgerTxResult = "tefMAX_LEDGER"
	// TecInsufficientReserveTxResult defines that reserve is insufficient to complete requested operation.
	TecInsufficientReserveTxResult = "tecINSUFFICIENT_RESERVE"
	// TelInsufFeeP defines that fee from the transaction is not high enough to meet the server's current transaction
	//	cost requirement, which is derived from its load level and network-level requirements. If the individual server
	//	is too busy to process your transaction right now, it may cache the transaction and automatically retry later.
	TelInsufFeeP = "telINSUF_FEE_P"
	// TecTxResultPrefix is `tec` prefix for tx result.
	TecTxResultPrefix = "tec"
	// TemTxResultPrefix is `tem` prefix for tx result.
	TemTxResultPrefix = "tem"
)

// Reserves.
var (
	ReserveToActivateAccount = float64(10)
	// ReservePerItem defines reserves of objects that count towards their owner's reserve requirement include:
	//	Checks, Deposit Preauthorizations, Escrows, NFT Offers, NFT Pages, Offers, Payment Channels, Signer Lists,
	//	Tickets, and Trust Lines.
	ReservePerItem = float64(2)
)

const (
	// KeyringSuffix is used as suffix for xrpl keyring.
	KeyringSuffix = "xrpl"
	// XRPLHDPath is the hd path used to derive xrpl keys.
	XRPLHDPath = "m/44'/144'/0'/0/0"
	// XRPLCoinType is the coin type of XRPL token.
	XRPLCoinType = 144
	// XRPLIssuedTokenDecimals is XRPL decimals used on the coreum.
	XRPLIssuedTokenDecimals = 15
	// XRPCurrencyDecimals is XRP decimals used on the coreum.
	XRPCurrencyDecimals = 6
	// MaxTicketsToAllocate is the max supported tickets count to allocate.
	MaxTicketsToAllocate = uint32(250)
	// MaxAllowedXRPLSigners max signers for the signers set.
	MaxAllowedXRPLSigners = uint32(32)
)

// XRP token constants.
var (
	XRPTokenIssuer   = rippledata.Account{}
	XRPTokenCurrency = rippledata.Currency{}
)
