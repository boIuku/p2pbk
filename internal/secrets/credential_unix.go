//go:build !windows

package secrets

import (
	"fmt"
	"os"
	"path/filepath"
)

// On Unix, systemd LoadCredential supplies a short-lived, mode-restricted
// plaintext file to the service. The source credential is owned and protected
// by systemd/the service administrator and is never written by p2pbk.
func WriteCredential(path, password string) error {
	return fmt.Errorf("credential creation is managed by systemd; use LoadCredential for %s", path)
}

func ReadCredential(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path))
}
