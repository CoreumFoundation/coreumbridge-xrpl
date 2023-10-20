package xrpl

import (
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/pkg/errors"
	ripplecrypto "github.com/rubblelabs/ripple/crypto"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
)

var (
	_       ripplecrypto.Key = &xrplPrivKey{}
	zeroSeq                  = lo.ToPtr(uint32(0))
)

// xrplPrivKey is `ripplecrypto.Key` implementation.
type xrplPrivKey struct {
	privKey cryptotypes.PrivKey
}

func newXRPLPrivKey(privKey cryptotypes.PrivKey) xrplPrivKey {
	return xrplPrivKey{
		privKey: privKey,
	}
}

//nolint:revive,stylecheck //interface method
func (k xrplPrivKey) Id(sequence *uint32) []byte {
	return ripplecrypto.Sha256RipeMD160(k.Public(sequence))
}

func (k xrplPrivKey) Private(_ *uint32) []byte {
	return k.privKey.Bytes()
}

func (k xrplPrivKey) Public(_ *uint32) []byte {
	return k.privKey.PubKey().Bytes()
}

// KeyringTxSigner is XRPL singer for the cosmos keyring.
type KeyringTxSigner struct {
	kr keyring.Keyring
}

// NewKeyringTxSigner returns new instance of the KeyringTxSigner.
func NewKeyringTxSigner(kr keyring.Keyring) *KeyringTxSigner {
	return &KeyringTxSigner{
		kr: kr,
	}
}

// Sign signs the xrpl tx.
func (s *KeyringTxSigner) Sign(tx rippledata.Transaction, keyName string) error {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return err
	}

	if err = rippledata.Sign(tx, key, zeroSeq); err != nil {
		return errors.Wrapf(err, "failed to sign XRPL transaction with keyring")
	}

	return nil
}

// MultiSign signs the transaction for the multi-signing.
func (s *KeyringTxSigner) MultiSign(tx rippledata.MultiSignable, keyName string) (rippledata.Signer, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.Signer{}, err
	}
	acc := extractAccountFromXRPLKey(key)
	if err := rippledata.MultiSign(tx, key, zeroSeq, acc); err != nil {
		return rippledata.Signer{}, err
	}

	return rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       acc,
			TxnSignature:  tx.GetSignature(),
			SigningPubKey: tx.GetPublicKey(),
		},
	}, nil
}

// Account returns account form the keyring.
func (s *KeyringTxSigner) Account(keyName string) (rippledata.Account, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.Account{}, err
	}

	return extractAccountFromXRPLKey(key), nil
}

// PubKey returns PubKey form the keyring.
func (s *KeyringTxSigner) PubKey(keyName string) (rippledata.PublicKey, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.PublicKey{}, err
	}

	return extractPubKeyFromXRPLKey(key), nil
}

// GetKeyring returns signer keyring.
func (s *KeyringTxSigner) GetKeyring() keyring.Keyring {
	return s.kr
}

func (s *KeyringTxSigner) extractXRPLPrivKey(keyName string) (xrplPrivKey, error) {
	key, err := s.kr.Key(keyName)
	if err != nil {
		return xrplPrivKey{}, errors.Wrapf(err, "failed to get key xrpl form the keyring, key name:%s", keyName)
	}
	rl := key.GetLocal()
	if rl.PrivKey == nil {
		return xrplPrivKey{}, errors.Errorf("private key is not available, key name:%s", keyName)
	}
	priv, ok := rl.PrivKey.GetCachedValue().(cryptotypes.PrivKey)
	if !ok {
		return xrplPrivKey{}, errors.New("unable to cast any to cryptotypes.PrivKey")
	}

	return newXRPLPrivKey(priv), nil
}

func extractAccountFromXRPLKey(key xrplPrivKey) rippledata.Account {
	var account rippledata.Account
	copy(account[:], key.Id(zeroSeq))
	return account
}

func extractPubKeyFromXRPLKey(key xrplPrivKey) rippledata.PublicKey {
	var pubKey rippledata.PublicKey
	copy(pubKey[:], key.Public(zeroSeq))
	return pubKey
}
