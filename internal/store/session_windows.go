//go:build windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var crypt32 = syscall.NewLazyDLL("crypt32.dll")
var protect = crypt32.NewProc("CryptProtectData")
var unprotect = crypt32.NewProc("CryptUnprotectData")
var localFree = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")

type blob struct {
	Size uint32
	Data *byte
}

func crypt(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty session data")
	}
	in := blob{uint32(len(data)), &data[0]}
	var out blob
	p := protect
	if decrypt {
		p = unprotect
	}
	r, _, err := p.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(data)
	if r == 0 {
		return nil, err
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func SaveSession(home, id string, data []byte) error {
	b, err := crypt(data, false)
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(home, "sessions", id+".dpapi"), b)
}
func LoadSession(home, id string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(home, "sessions", id+".dpapi"))
	if err != nil {
		return nil, err
	}
	return crypt(b, true)
}
func DeleteSession(home, id string) error {
	err := os.Remove(filepath.Join(home, "sessions", id+".dpapi"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func SessionBackend() string { return "Windows DPAPI (current user)" }

func SavePassword(home, id string, password []byte) error {
	b, err := crypt(password, false)
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(home, "credentials", id+".dpapi"), b)
}
func LoadPassword(home, id string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(home, "credentials", id+".dpapi"))
	if err != nil {
		return nil, err
	}
	return crypt(b, true)
}
func DeletePassword(home, id string) error {
	err := os.Remove(filepath.Join(home, "credentials", id+".dpapi"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
