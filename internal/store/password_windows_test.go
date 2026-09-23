//go:build windows

package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRememberedPasswordEncryptedAndDeleted(t *testing.T) {
	home := t.TempDir()
	id := Identity(Target{URL: "http://test", User: "test"})
	password := []byte("example-secret")
	if err := SavePassword(home, id, password); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(home, "credentials", id+".dpapi"))
	if err != nil || bytes.Contains(onDisk, password) {
		t.Fatalf("password not protected on disk: %v", err)
	}
	got, err := LoadPassword(home, id)
	if err != nil || !bytes.Equal(got, password) {
		t.Fatalf("password did not round trip: %v", err)
	}
	if err := DeletePassword(home, id); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPassword(home, id); err == nil {
		t.Fatal("password still readable after deletion")
	}
}
