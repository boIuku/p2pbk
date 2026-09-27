package secrets

import (
	"errors"
	"os"
	"strings"
	"sync"
)

const PasswordEnv = "P2PBK_REPOSITORY_PASSWORD"

var envMu sync.Mutex

func FromEnv() string {
	return os.Getenv(PasswordEnv)
}

func FromEnvOrFile(path string) (string, error) {
	if value := FromEnv(); value != "" {
		return value, nil
	}
	if path == "" {
		return "", nil
	}
	data, err := ReadCredential(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("service credential is empty")
	}
	return value, nil
}

func Env(password string) []string {
	env := os.Environ()
	if password == "" {
		return env
	}
	return append(env, PasswordEnv+"="+password)
}

func WithPassword[T any](password string, fn func() (T, error)) (T, error) {
	if password == "" {
		return fn()
	}
	if os.Getenv(PasswordEnv) == password {
		return fn()
	}
	envMu.Lock()
	defer envMu.Unlock()

	prior, hadPrior := os.LookupEnv(PasswordEnv)
	if err := os.Setenv(PasswordEnv, password); err != nil {
		var zero T
		return zero, err
	}
	out, err := fn()
	if hadPrior {
		_ = os.Setenv(PasswordEnv, prior)
	} else {
		_ = os.Unsetenv(PasswordEnv)
	}
	return out, err
}
