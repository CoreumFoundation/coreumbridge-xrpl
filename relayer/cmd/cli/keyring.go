package cli

import (
	"context"
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

func newCacheKeyring(
	name string,
	parentKeyring keyring.Keyring,
	cdc codec.Codec,
	log logger.Logger,
) keyring.Keyring {
	return &cacheKeyring{
		name:          name,
		log:           log,
		parentKeyring: parentKeyring,
		cacheKeyring:  keyring.NewInMemory(cdc),
	}
}

type cacheKeyring struct {
	name string
	log  logger.Logger

	mu            sync.Mutex
	parentKeyring keyring.Keyring
	cacheKeyring  keyring.Keyring
}

// Backend returns the backend type used in the keyring config: "file", "os", "kwallet", "pass", "test", "memory".
func (ck *cacheKeyring) Backend() string {
	return ck.parentKeyring.Backend()
}

// List lists all keys.
func (ck *cacheKeyring) List() ([]*keyring.Record, error) {
	return ck.parentKeyring.List()
}

// SupportedAlgorithms returns supported signing algorithms for Keyring and Ledger respectively.
func (ck *cacheKeyring) SupportedAlgorithms() (keyring.SigningAlgoList, keyring.SigningAlgoList) {
	return ck.parentKeyring.SupportedAlgorithms()
}

// Key return key by uid.
func (ck *cacheKeyring) Key(uid string) (*keyring.Record, error) {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	keyInfo, err := ck.cacheKeyring.Key(uid)
	if err != nil {
		ck.log.Info(context.Background(), "Access to keyring requested", zap.String("keyring", ck.name),
			zap.String("key", uid))

		keyInfo, err = ck.parentKeyring.Key(uid)
		if err != nil {
			return nil, err
		}
		if err := ck.cacheKey(keyInfo); err != nil {
			return nil, err
		}
	}

	return keyInfo, nil
}

// KeyByAddress returns key by address.
func (ck *cacheKeyring) KeyByAddress(address sdk.Address) (*keyring.Record, error) {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	keyInfo, err := ck.cacheKeyring.KeyByAddress(address)
	if err != nil {
		ck.log.Info(context.Background(), "Access to keyring requested", zap.String("keyring", ck.name),
			zap.Stringer("address", address))

		keyInfo, err = ck.parentKeyring.KeyByAddress(address)
		if err != nil {
			return nil, err
		}
		if err := ck.cacheKey(keyInfo); err != nil {
			return nil, err
		}
	}

	return keyInfo, nil
}

// Delete removes key from the keyring.
func (ck *cacheKeyring) Delete(uid string) error {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	if err := ck.parentKeyring.Delete(uid); err != nil {
		return err
	}
	_ = ck.cacheKeyring.Delete(uid)
	return nil
}

// DeleteByAddress removes key from the keyring.
func (ck *cacheKeyring) DeleteByAddress(address sdk.Address) error {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	if err := ck.parentKeyring.DeleteByAddress(address); err != nil {
		return err
	}
	_ = ck.cacheKeyring.DeleteByAddress(address)
	return nil
}

// Rename renames an existing key in the keyring.
func (ck *cacheKeyring) Rename(from string, to string) error {
	ck.mu.Lock()
	defer ck.mu.Unlock()

	if err := ck.parentKeyring.Rename(from, to); err != nil {
		return err
	}
	_ = ck.cacheKeyring.Delete(from)
	return nil
}

// NewMnemonic generates a new mnemonic, derives a hierarchical deterministic key from it, and
// persists the key to storage. Returns the generated mnemonic and the key Info.
// It returns an error if it fails to generate a key for the given algo type, or if
// another key is already stored under the same name or address.
//
// A passphrase set to the empty string will set the passphrase to the DefaultBIP39Passphrase value.
func (ck *cacheKeyring) NewMnemonic(
	uid string,
	language keyring.Language,
	hdPath, bip39Passphrase string,
	algo keyring.SignatureAlgo,
) (*keyring.Record, string, error) {
	return ck.parentKeyring.NewMnemonic(uid, language, hdPath, bip39Passphrase, algo)
}

// NewAccount converts a mnemonic to a private key and BIP-39 HD Path and persists it.
// It fails if there is an existing key Info with the same address.
func (ck *cacheKeyring) NewAccount(
	uid, mnemonic, bip39Passphrase, hdPath string,
	algo keyring.SignatureAlgo,
) (*keyring.Record, error) {
	return ck.parentKeyring.NewAccount(uid, mnemonic, bip39Passphrase, hdPath, algo)
}

// SaveLedgerKey retrieves a public key reference from a Ledger device and persists it.
func (ck *cacheKeyring) SaveLedgerKey(
	uid string,
	algo keyring.SignatureAlgo,
	hrp string,
	coinType, account, index uint32,
) (*keyring.Record, error) {
	return ck.parentKeyring.SaveLedgerKey(uid, algo, hrp, coinType, account, index)
}

// SaveOfflineKey stores a public key and returns the persisted Info structure.
func (ck *cacheKeyring) SaveOfflineKey(uid string, pubkey types.PubKey) (*keyring.Record, error) {
	return ck.parentKeyring.SaveOfflineKey(uid, pubkey)
}

// SaveMultisig stores and returns a new multsig (offline) key reference.
func (ck *cacheKeyring) SaveMultisig(uid string, pubkey types.PubKey) (*keyring.Record, error) {
	return ck.parentKeyring.SaveMultisig(uid, pubkey)
}

// Sign signs byte message with a user key.
func (ck *cacheKeyring) Sign(uid string, msg []byte) ([]byte, types.PubKey, error) {
	if _, err := ck.Key(uid); err != nil {
		return nil, nil, err
	}

	return ck.cacheKeyring.Sign(uid, msg)
}

// SignByAddress signs byte message with a user key providing the address.
func (ck *cacheKeyring) SignByAddress(address sdk.Address, msg []byte) ([]byte, types.PubKey, error) {
	if _, err := ck.KeyByAddress(address); err != nil {
		return nil, nil, err
	}

	return ck.cacheKeyring.SignByAddress(address, msg)
}

// ImportPrivKey imports ASCII armored passphrase-encrypted private keys.
func (ck *cacheKeyring) ImportPrivKey(uid, armor, passphrase string) error {
	return ck.parentKeyring.ImportPrivKey(uid, armor, passphrase)
}

// ImportPrivKeyHex imports hex encoded keys.
func (ck *cacheKeyring) ImportPrivKeyHex(uid, privKey, algoStr string) error {
	return ck.parentKeyring.ImportPrivKeyHex(uid, privKey, algoStr)
}

// ImportPubKey imports ASCII armored public keys.
func (ck *cacheKeyring) ImportPubKey(uid string, armor string) error {
	return ck.parentKeyring.ImportPubKey(uid, armor)
}

// ExportPubKeyArmor exports public key.
func (ck *cacheKeyring) ExportPubKeyArmor(uid string) (string, error) {
	return ck.parentKeyring.ExportPubKeyArmor(uid)
}

// ExportPubKeyArmorByAddress exports public key.
func (ck *cacheKeyring) ExportPubKeyArmorByAddress(address sdk.Address) (string, error) {
	return ck.parentKeyring.ExportPubKeyArmorByAddress(address)
}

// ExportPrivKeyArmor returns a private key in ASCII armored format.
func (ck *cacheKeyring) ExportPrivKeyArmor(uid, encryptPassphrase string) (armor string, err error) {
	return ck.parentKeyring.ExportPrivKeyArmor(uid, encryptPassphrase)
}

// ExportPrivKeyArmorByAddress returns a private key in ASCII armored format.
func (ck *cacheKeyring) ExportPrivKeyArmorByAddress(
	address sdk.Address,
	encryptPassphrase string,
) (armor string, err error) {
	return ck.parentKeyring.ExportPrivKeyArmorByAddress(address, encryptPassphrase)
}

// MigrateAll migrates all keys from legacy format.
func (ck *cacheKeyring) MigrateAll() ([]*keyring.Record, error) {
	return ck.parentKeyring.MigrateAll()
}

func (ck *cacheKeyring) cacheKey(keyInfo *keyring.Record) error {
	pass := uuid.NewString()
	armor, err := ck.parentKeyring.ExportPrivKeyArmor(keyInfo.Name, pass)
	if err != nil {
		return err
	}
	return ck.cacheKeyring.ImportPrivKey(keyInfo.Name, armor, pass)
}
