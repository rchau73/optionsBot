package keystore

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The master key's two sources. Only this package reads them
// (tests/keystore_test.go fails on any other .go file naming them), so the
// key reaches no other code. Neither is ever in the repository or
// .env.example (the variable name only, empty).
const (
	masterKeyEnv     = "KEYSTORE_MASTER_KEY"      // the key itself, base64
	masterKeyFileEnv = "KEYSTORE_MASTER_KEY_FILE" // a path to an owner-only file holding it, base64
)

// MinMasterKeyBytes is the shortest master key the store accepts (256 bits).
const MinMasterKeyBytes = 32

var (
	// ErrNoMasterKey: no master key was given; the store refuses to open.
	ErrNoMasterKey = errors.New("keystore: no master key: set " + masterKeyEnv + " (base64 of at least 32 random bytes: openssl rand -base64 32) or " + masterKeyFileEnv)
	// ErrShortMasterKey: the master key has fewer than MinMasterKeyBytes bytes.
	ErrShortMasterKey = fmt.Errorf("keystore: master key shorter than %d bytes", MinMasterKeyBytes)
)

// LoadMasterKey reads the master key from the environment: KEYSTORE_MASTER_KEY,
// or else the file KEYSTORE_MASTER_KEY_FILE names (owner-only, like the store).
// Both hold base64; the decoded key must have at least MinMasterKeyBytes bytes.
// Setting both is refused, so it is never unclear which key sealed the store.
func LoadMasterKey() ([]byte, error) {
	value, path := os.Getenv(masterKeyEnv), os.Getenv(masterKeyFileEnv)
	switch {
	case value != "" && path != "":
		return nil, fmt.Errorf("keystore: set %s or %s, not both", masterKeyEnv, masterKeyFileEnv)
	case value == "" && path == "":
		return nil, ErrNoMasterKey
	case path != "":
		if err := checkOwnerOnly(path); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("keystore: read master key file: %w", err)
		}
		value = string(raw)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		// The decoder's error names only an offset, never the value.
		return nil, fmt.Errorf("keystore: master key is not base64: %w", err)
	}
	if err := checkMasterKey(key); err != nil {
		return nil, err
	}
	return key, nil
}

func checkMasterKey(key []byte) error {
	if len(key) == 0 {
		return ErrNoMasterKey
	}
	if len(key) < MinMasterKeyBytes {
		return ErrShortMasterKey
	}
	return nil
}
