//go:build !windows

package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/zalando/go-keyring"
)

func service(home string) string {
	s := sha256.Sum256([]byte(home))
	return "onekvm-cli-" + hex.EncodeToString(s[:8])
}
func SaveSession(home, id string, b []byte) error { return keyring.Set(service(home), id, string(b)) }
func LoadSession(home, id string) ([]byte, error) {
	v, err := keyring.Get(service(home), id)
	return []byte(v), err
}
func DeleteSession(home, id string) error {
	err := keyring.Delete(service(home), id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
func SessionBackend() string { return "system keyring (no plaintext fallback)" }
