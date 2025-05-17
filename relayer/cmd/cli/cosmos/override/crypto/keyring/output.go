package keyring

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ripplecrypto "github.com/rubblelabs/ripple/crypto"
	rippledata "github.com/rubblelabs/ripple/data"
)

// AddressFormatter defines the address formatter.
type AddressFormatter func(publicKey cryptotypes.PubKey) sdk.Address

// SelectedAddressFormatter stores the address formatter used by the key commands.
var SelectedAddressFormatter AddressFormatter = CoreumAddressFormatter

// CoreumAddressFormatter formats coreum addresses.
func CoreumAddressFormatter(publicKey cryptotypes.PubKey) sdk.Address {
	return sdk.AccAddress(publicKey.Address())
}

// XRPLAddressFormatter formats XRPL addresses.
func XRPLAddressFormatter(publicKey cryptotypes.PubKey) sdk.Address {
	return xrplAddress{publicKey: publicKey}
}

type xrplAddress struct {
	publicKey cryptotypes.PubKey
}

func (a xrplAddress) String() string {
	var account rippledata.Account
	copy(account[:], ripplecrypto.Sha256RipeMD160(a.publicKey.Bytes()))
	return account.String()
}

func (a xrplAddress) Equals(sdk.Address) bool {
	panic("not implemented")
}

func (a xrplAddress) Empty() bool {
	panic("not implemented")
}

func (a xrplAddress) Marshal() ([]byte, error) {
	panic("not implemented")
}

func (a xrplAddress) MarshalJSON() ([]byte, error) {
	panic("not implemented")
}

func (a xrplAddress) Bytes() []byte {
	panic("not implemented")
}

func (a xrplAddress) Format(s fmt.State, verb rune) {
	panic("not implemented")
}

// MkAccKeyOutput create a KeyOutput in with "acc" Bech32 prefixes. If the
// public key is a multisig public key, then the threshold and constituent
// public keys will be added.
func MkAccKeyOutput(k *keyring.Record) (keys.KeyOutput, error) {
	pk, err := k.GetPubKey()
	if err != nil {
		return keys.KeyOutput{}, err
	}
	return keys.NewKeyOutput(k.Name, k.GetType(), SelectedAddressFormatter(pk), pk)
}

// MkAccKeysOutput returns a slice of KeyOutput objects, each with the "acc"
// Bech32 prefixes, given a slice of Record objects. It returns an error if any
// call to MkKeyOutput fails.
func MkAccKeysOutput(records []*keyring.Record) ([]keys.KeyOutput, error) {
	kos := make([]keys.KeyOutput, len(records))
	var err error
	for i, r := range records {
		kos[i], err = MkAccKeyOutput(r)
		if err != nil {
			return nil, err
		}
	}

	return kos, nil
}
