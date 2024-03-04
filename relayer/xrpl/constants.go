package xrpl

import (
	rippledata "github.com/rubblelabs/ripple/data"
)

// Error codes.
const (
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
	// XRPLHDPath is the hd path used to derive xrpl keys.
	XRPLHDPath = "m/44'/144'/0'/0/0"
	// CoinType is the coin type of XRPL token.
	CoinType = 144
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
