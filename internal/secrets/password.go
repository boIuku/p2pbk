package secrets

import (
	"os"
	"sync"
)

const PasswordEnv = "P2PBK_REPOSITORY_PASSWORD"

var envMu sync.Mutex

func FromEnv() string {
	return os.Getenv(PasswordEnv)
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
