package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/mod/sumdb/dirhash"

	"github.com/CoreumFoundation/coreum-tools/pkg/build"
	"github.com/CoreumFoundation/coreum-tools/pkg/logger"
	"github.com/CoreumFoundation/crust/build/rust"
	"github.com/CoreumFoundation/crust/build/tools"
)

// BuildSmartContract builds bridge smart contract.
func BuildSmartContract(ctx context.Context, deps build.DepsFunc) error {
	deps(CompileSmartContract(filepath.Join(repoPath, "contract")))
	return nil
}

// FIXME (wojciech): Code copied from coreum, let's move it to crust

// CompileSmartContract returns function compiling smart contract.
func CompileSmartContract(codeDirPath string) build.CommandFunc {
	return func(ctx context.Context, deps build.DepsFunc) error {
		log := logger.Get(ctx)
		log.Info("Compiling WASM smart contract", zap.String("path", codeDirPath))

		codeDirAbsPath, err := filepath.Abs(codeDirPath)
		if err != nil {
			return errors.WithStack(err)
		}

		contractSrcHash, err := computeContractSrcHash(codeDirAbsPath)
		if err != nil {
			return errors.WithStack(err)
		}

		wasmCachePath := filepath.Join(tools.CacheDir(), "wasm")
		if err := os.MkdirAll(wasmCachePath, 0o700); err != nil {
			return errors.WithStack(err)
		}

		codeHashesFile, err := os.OpenFile(filepath.Join(wasmCachePath, "code-hashes.json"), os.O_CREATE|os.O_RDWR, 0o700)
		if err != nil {
			return errors.WithStack(err)
		}
		defer codeHashesFile.Close()

		codeHashesBytes, err := io.ReadAll(codeHashesFile)
		if err != nil {
			return errors.WithStack(err)
		}
		absPathHash := fmt.Sprintf("%x", sha256.Sum256([]byte(codeDirAbsPath)))

		storedCodeHashes := make(map[string]string)
		if len(codeHashesBytes) != 0 {
			err := json.Unmarshal(codeHashesBytes, &storedCodeHashes)
			if err != nil {
				return errors.WithStack(err)
			}
		}

		if storedHash, ok := storedCodeHashes[absPathHash]; ok {
			contractArtifactsHash, err := computeContractArtifactsHash(codeDirAbsPath)
			if err != nil {
				return err
			}
			codeHash := contractSrcHash + contractArtifactsHash
			log.Info("Computed contract code hash", zap.String("hash", codeHash))
			if codeHash == storedHash {
				log.Info("No changes in the contract, skipping compilation.")
				return nil
			}
		}

		targetCachePath := filepath.Join(wasmCachePath, "targets", absPathHash)
		if err := os.MkdirAll(targetCachePath, 0o700); err != nil {
			return errors.WithStack(err)
		}

		registryCachePath := filepath.Join(wasmCachePath, "registry")
		if err := os.MkdirAll(registryCachePath, 0o700); err != nil {
			return errors.WithStack(err)
		}

		if err := rust.BuildSmartContract(ctx, deps, codeDirAbsPath); err != nil {
			return err
		}

		contractArtifactsHash, err := computeContractArtifactsHash(codeDirAbsPath)
		if err != nil {
			return err
		}
		if contractArtifactsHash == "" {
			return errors.New("artifacts folder doesn't exist after the contract building")
		}

		newCodeHash := contractSrcHash + contractArtifactsHash
		storedCodeHashes[absPathHash] = newCodeHash
		codeHashesBytes, err = json.Marshal(storedCodeHashes)
		if err != nil {
			return errors.WithStack(err)
		}

		return replaceFileContent(codeHashesFile, codeHashesBytes)
	}
}

func computeContractSrcHash(path string) (string, error) {
	hash, err := dirhash.HashDir(filepath.Join(path, "src"), "", dirhash.Hash1)
	if err != nil {
		return "", errors.WithStack(err)
	}

	return hash, nil
}

func computeContractArtifactsHash(path string) (string, error) {
	hash, err := dirhash.HashDir(filepath.Join(path, "artifacts"), "", dirhash.Hash1)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.WithStack(err)
	}

	return hash, nil
}

func replaceFileContent(codeHashesFile *os.File, codeHashesBytes []byte) error {
	if err := codeHashesFile.Truncate(0); err != nil {
		return errors.WithStack(err)
	}
	if _, err := codeHashesFile.Seek(0, 0); err != nil {
		return errors.WithStack(err)
	}
	if _, err := codeHashesFile.Write(codeHashesBytes); err != nil {
		return errors.WithStack(err)
	}

	return nil
}
