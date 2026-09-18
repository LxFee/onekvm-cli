//go:build windows

package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDPAPIEncryptedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	secret := []byte(`{"token":"test-session-secret"}`)
	id := Identity(Target{URL: "http://test", User: "test"})
	if err := SaveSession(dir, id, secret); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(dir, "sessions", id+".dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("test-session-secret")) {
		t.Fatal("plaintext credential on disk")
	}
	got, err := LoadSession(dir, id)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("DPAPI roundtrip failed: %v", err)
	}
	if err := DeleteSession(dir, id); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(dir, id); err == nil {
		t.Fatal("session not deleted")
	}
}
