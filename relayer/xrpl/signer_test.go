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

func TestKeyringTxSigner_Sign_And_MultiSign(t *testing.T) {
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

	xrpPaymentTx := rippledata.Payment{
		Destination: *recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			Account:         signerAcc,
			Sequence:        1,
			TransactionType: rippledata.PAYMENT,
		},
	}
	// check that signature is correct
	require.NoError(t, signer.Sign(&xrpPaymentTx, keyName))
	require.Equal(t, "30440220744D7F83BA64809384164814DAED4F54732303C783BCD5433356B4A9962F8E5702205548A956AB57B17B0A93A5E932CB0C2E7BCA060B660222989E6163E1C0C8BF00", xrpPaymentTx.TxnSignature.String())

	// update tx for the multi-sign
	xrpPaymentTx = rippledata.Payment{
		Destination: *recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			Account:         signerAcc,
			TransactionType: rippledata.PAYMENT,
			SigningPubKey:   &rippledata.PublicKey{},
		},
	}

	txSigner, err := signer.MultiSign(&xrpPaymentTx, keyName)
	require.NoError(t, err)
	require.Equal(t, "304402206FE783A8D4BEED7146189E35015CA3BC32A27330118F26B3D3CEF4C6948292FA022046CE786087B61AA46903867178C13963B6A6C308AB758DD6A9BEABB9EDABE52D", txSigner.Signer.TxnSignature.String())
}
