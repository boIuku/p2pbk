//go:build windows

package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
	"p2pbk/internal/durable"
)

const cryptProtectLocalMachine uint32 = 0x4

func WriteCredential(path, password string) error {
	if password == "" {
		return fmt.Errorf("repository password is required")
	}
	input := []byte(password)
	blob := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var encrypted windows.DataBlob
	if err := windows.CryptProtectData(&blob, nil, nil, 0, nil, cryptProtectLocalMachine, &encrypted); err != nil {
		return fmt.Errorf("protect service credential: %w", err)
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(encrypted.Data))))
	data := unsafe.Slice(encrypted.Data, encrypted.Size)
	return durable.WriteFile(path, append([]byte(nil), data...), 0o600)
}

func ReadCredential(path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("service credential is empty")
	}
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var decrypted windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, 0, &decrypted); err != nil {
		return nil, fmt.Errorf("unprotect service credential: %w", err)
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(decrypted.Data))))
	return append([]byte(nil), unsafe.Slice(decrypted.Data, decrypted.Size)...), nil
}
