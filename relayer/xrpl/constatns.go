package xrpl

const (
	// TecPathDryTxResult defines that provided paths did not have enough liquidity to send anything at all.
	//	This could mean that the source and destination accounts are not linked by trust lines.
	TecPathDryTxResult = "tecPATH_DRY"
	// TefNOTicketTxResult defines the result which indicates the usage of the passed ticket or not created ticket.
	TefNOTicketTxResult = "tefNO_TICKET"
	// TefPastSeqTxResult defines the result which indicates the usage of the sequence in the past.
	TefPastSeqTxResult = "tefPAST_SEQ"
	// TerPreSeqTxResult defines the result which indicates the usage of the sequence in the future.
	TerPreSeqTxResult = "terPRE_SEQ"
	// TecInsufficientReserveTxResult defines the result which indicates the insufficient reserve to complete requested operation.
	TecInsufficientReserveTxResult = "tecINSUFFICIENT_RESERVE"
)
