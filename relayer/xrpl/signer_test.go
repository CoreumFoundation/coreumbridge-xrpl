package xrpl_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumapp "github.com/CoreumFoundation/coreum/v3/app"
	coreumconfig "github.com/CoreumFoundation/coreum/v3/pkg/config"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

//nolint:lll // TODO(dzmitryhil) linter length limit
func TestKeyringTxSigner_MultiSignWithSignatureVerification(t *testing.T) {
	t.Parallel()

	encodingConfig := coreumconfig.NewEncodingConfig(coreumapp.ModuleBasics)
	kr := keyring.NewInMemory(encodingConfig.Codec)
	const keyName = "xrpl"
	_, err := kr.NewAccount(
		keyName,
		"bring exhibit fancy tomorrow frequent pink athlete win magnet mail staff riot dune luxury slow own arrest unfair beyond trip deliver hazard nerve bicycle",
		"",
		sdk.GetConfig().GetFullBIP44Path(),
		hd.Secp256k1,
	)
	require.NoError(t, err)

	signer := xrpl.NewKeyringTxSigner(kr)
	signerAcc, err := signer.Account(keyName)
	require.NoError(t, err)
	// check that account is expected correct
	require.Equal(t, "rK7uyssdUF8Uw6eNTXhAchN1x8pTi5JM2C", signerAcc.String())

	recipientAccount, err := rippledata.NewAccountFromAddress("rnZfuixFVhyAXWZDnYsCGEg2zGtpg4ZjKn")
	require.NoError(t, err)
	xrpAmount, err := rippledata.NewAmount("100000")
	require.NoError(t, err)

	xrpPaymentTx := buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	// check that signature is correct
	require.NoError(t, signer.Sign(&xrpPaymentTx, keyName))
	require.Equal(t, "30440220744D7F83BA64809384164814DAED4F54732303C783BCD5433356B4A9962F8E5702205548A956AB57B17B0A93A5E932CB0C2E7BCA060B660222989E6163E1C0C8BF00", xrpPaymentTx.TxnSignature.String())

	valid, err := rippledata.CheckSignature(&xrpPaymentTx)
	require.NoError(t, err)
	require.True(t, valid)

	// update tx for the multi-sign
	xrpPaymentTx = buildPaymentTx(recipientAccount, xrpAmount, signerAcc)
	txSigner, err := signer.MultiSign(&xrpPaymentTx, keyName)
	require.NoError(t, err)
	require.Equal(t, "304402201F5A4835DA5C525A6FF0F3689139FA1519325B9018E62C44D7903F553671350A02204250230E601C929063634B6B121C2F95E4D8588902487D4F9182BB43F01FAC3C", txSigner.Signer.TxnSignature.String())

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

//nolint:lll // TODO(dzmitryhil) linter length limit
func buildPaymentTx(recipientAccount *rippledata.Account, xrpAmount *rippledata.Amount, signerAcc rippledata.Account) rippledata.Payment {
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
