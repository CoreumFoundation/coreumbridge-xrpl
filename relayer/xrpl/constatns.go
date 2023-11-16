package xrpl

const (
	// TefNOTicketTxResult defines that the usage of the passed ticket or not created ticket.
	TefNOTicketTxResult = "tefNO_TICKET"
	// TefPastSeqTxResult defines that the usage of the sequence in the past.
	TefPastSeqTxResult = "tefPAST_SEQ"
	// TefMaxLedgerTxResult defines that ledger sequence too high.
	TefMaxLedgerTxResult = "tefMAX_LEDGER"
	// TecInsufficientReserveTxResult defines that reserve is insufficient to complete requested operation.
	TecInsufficientReserveTxResult = "tecINSUFFICIENT_RESERVE"
	// TecTxResultPrefix is `tec` prefix for tx result.
	TecTxResultPrefix = "tec"
	// TemTxResultPrefix is `tem` prefix for tx result.
	TemTxResultPrefix = "tem"
)
