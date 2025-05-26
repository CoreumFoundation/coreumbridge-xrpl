package xrpl_test

import (
	"testing"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/x/auth"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumconfig "github.com/CoreumFoundation/coreum/v5/pkg/config"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

//nolint:lll // test contains mnemonics and long hashes.
func TestKeyringTxSigner_MultiSignWithSignatureVerification(t *testing.T) {
	t.Parallel()

	encodingConfig := coreumconfig.NewEncodingConfig(auth.AppModuleBasic{}, wasm.AppModuleBasic{})
	kr := keyring.NewInMemory(encodingConfig.Codec)
	const keyName = "xrpl"
	_, err := kr.NewAccount(
		keyName,
		"bring exhibit fancy tomorrow frequent pink athlete win magnet mail staff riot dune luxury slow own arrest unfair beyond trip deliver hazard nerve bicycle",
		"",
		xrpl.XRPLHDPath,
		hd.Secp256k1,
	)
	require.NoError(t, err)

	signer := xrpl.NewKeyringTxSigner(kr)
	signerAcc, err := signer.Account(keyName)
	require.NoError(t, err)
	// check that account is expected correct
	require.Equal(t, "rBprNyH2iH7Sqagi268aJuMubPB7XLjL1i", signerAcc.String())

	recipientAccount, err := rippledata.NewAccountFromAddress("rnZfuixFVhyAXWZDnYsCGEg2zGtpg4ZjKn")
	require.NoError(t, err)
	xrpAmount, err := rippledata.NewAmount("100000")
	require.NoError(t, err)

	xrpPaymentTx := buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	// check that signature is correct
	require.NoError(t, signer.Sign(&xrpPaymentTx, keyName))
	require.Equal(t, "3044022005DD15BDB2054B5F9B295EA3357B490AE99A31BA3EAC21A21B33E4E03E082DFD02202DCC8E915C0FAA026DA5477B9822BB449CB22EADF37F98571841B57DC86F3AAD", xrpPaymentTx.TxnSignature.String())

	valid, err := rippledata.CheckSignature(&xrpPaymentTx)
	require.NoError(t, err)
	require.True(t, valid)

	// update tx for the multi-sign
	xrpPaymentTx = buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	txSigner, err := signer.MultiSign(&xrpPaymentTx, keyName)
	require.NoError(t, err)
	require.Equal(t, "3045022100B4B3BBD3FC9A475D185C85810012686334F11382F47193DC2C680F6950635712022044C0AD58016A2B469A409C06DBE0FEC1824199125F29CDB7FB887ABB53E43D0F", txSigner.Signer.TxnSignature.String())

	xrpPaymentTx = buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, txSigner))
	valid, _, err = rippledata.CheckMultiSignature(&xrpPaymentTx)
	require.NoError(t, err)
	require.True(t, valid)
}

func TestPrivKeyTxSigner_MultiSignWithSignatureVerification(t *testing.T) {
	t.Parallel()

	signer := xrpl.GenPrivKeyTxSigner()
	signerAcc := signer.Account()

	recipientAccount, err := rippledata.NewAccountFromAddress("rnZfuixFVhyAXWZDnYsCGEg2zGtpg4ZjKn")
	require.NoError(t, err)
	xrpAmount, err := rippledata.NewAmount("100000")
	require.NoError(t, err)

	xrpPaymentTx := buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	// check that signature is correct
	require.NoError(t, signer.Sign(&xrpPaymentTx))
	valid, err := rippledata.CheckSignature(&xrpPaymentTx)
	require.NoError(t, err)
	require.True(t, valid)

	// update tx for the multi-sign
	xrpPaymentTx = buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	txSigner, err := signer.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, txSigner))
	valid, _, err = rippledata.CheckMultiSignature(&xrpPaymentTx)
	require.NoError(t, err)
	require.True(t, valid)
}

func buildPaymentTx(
	recipientAccount *rippledata.Account,
	xrpAmount *rippledata.Amount,
	signerAcc rippledata.Account,
) rippledata.Payment {
	return rippledata.Payment{
		Destination: *recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			Account:         signerAcc,
			Sequence:        1,
			TransactionType: rippledata.PAYMENT,
			// important for the multi-signing
			SigningPubKey: &rippledata.PublicKey{},
		},
	}
}
