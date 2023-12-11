package xrpl

import (
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
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

// ********** xrplPrivKey **********

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

func (k xrplPrivKey) ExtractAccountFromXRPLKey() rippledata.Account {
	var account rippledata.Account
	copy(account[:], k.Id(zeroSeq))
	return account
}

func (k xrplPrivKey) ExtractPubKeyFromXRPLKey() rippledata.PublicKey {
	var pubKey rippledata.PublicKey
	copy(pubKey[:], k.Public(zeroSeq))
	return pubKey
}

// ********** KeyringTxSigner **********

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

// Sign signs the transaction with the provided key name.
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

// MultiSign signs the transaction for the multi-signing with the provided key name.
func (s *KeyringTxSigner) MultiSign(tx rippledata.MultiSignable, keyName string) (rippledata.Signer, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.Signer{}, err
	}
	acc := key.ExtractAccountFromXRPLKey()
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

// Account returns account from the keyring for the provided key name.
func (s *KeyringTxSigner) Account(keyName string) (rippledata.Account, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.Account{}, err
	}

	return key.ExtractAccountFromXRPLKey(), nil
}

// PubKey returns PubKey from the keyring for the provided key name.
func (s *KeyringTxSigner) PubKey(keyName string) (rippledata.PublicKey, error) {
	key, err := s.extractXRPLPrivKey(keyName)
	if err != nil {
		return rippledata.PublicKey{}, err
	}

	return key.ExtractPubKeyFromXRPLKey(), nil
}

// GetKeyring returns signer keyring.
func (s *KeyringTxSigner) GetKeyring() keyring.Keyring {
	return s.kr
}

func (s *KeyringTxSigner) extractXRPLPrivKey(keyName string) (xrplPrivKey, error) {
	key, err := s.kr.Key(keyName)
	if err != nil {
		return xrplPrivKey{}, errors.Wrapf(err, "failed to get key from the keyring, key name:%s", keyName)
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

// ********** PrivKeyTxSigner **********

// PrivKeyTxSigner is XRPL singer for the set priv key.
type PrivKeyTxSigner struct {
	key xrplPrivKey
}

// GenPrivKeyTxSigner returns new generated instance of the PrivKeyTxSigner.
func GenPrivKeyTxSigner() *PrivKeyTxSigner {
	return NewPrivKeyTxSigner(secp256k1.GenPrivKey())
}

// NewPrivKeyTxSigner returns new instance of the PrivKeyTxSigner.
func NewPrivKeyTxSigner(privKey cryptotypes.PrivKey) *PrivKeyTxSigner {
	return &PrivKeyTxSigner{
		key: newXRPLPrivKey(privKey),
	}
}

// Sign signs the transaction.
func (s *PrivKeyTxSigner) Sign(tx rippledata.Transaction) error {
	if err := rippledata.Sign(tx, s.key, zeroSeq); err != nil {
		return errors.Wrapf(err, "failed to sign XRPL transaction with keyring")
	}

	return nil
}

// MultiSign signs the transaction for the multi-signing.
func (s *PrivKeyTxSigner) MultiSign(tx rippledata.MultiSignable) (rippledata.Signer, error) {
	acc := s.key.ExtractAccountFromXRPLKey()
	if err := rippledata.MultiSign(tx, s.key, zeroSeq, acc); err != nil {
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

// Account returns account from the key.
func (s *PrivKeyTxSigner) Account() rippledata.Account {
	return s.key.ExtractAccountFromXRPLKey()
}

// PubKey returns PubKey of the key.
func (s *PrivKeyTxSigner) PubKey() rippledata.PublicKey {
	return s.key.ExtractPubKeyFromXRPLKey()
}
