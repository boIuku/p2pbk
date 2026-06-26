package secrets

import (
	"os"
	"testing"
)

func TestWithPasswordSetsAndRestoresRepositoryPassword(t *testing.T) {
	t.Setenv(PasswordEnv, "outer")

	got, err := WithPassword("inner", func() (string, error) {
		return FromEnv(), nil
	})
	if err != nil {
		t.Fatalf("WithPassword returned error: %v", err)
	}
	if got != "inner" {
		t.Fatalf("password during callback = %q, want inner", got)
	}
	if after := os.Getenv(PasswordEnv); after != "outer" {
		t.Fatalf("password after callback = %q, want outer", after)
	}
}

func TestEnvUsesRepositoryPasswordOnly(t *testing.T) {
	env := Env("secret")
	for _, value := range env {
		if value == PasswordEnv+"=secret" {
			return
		}
	}
	t.Fatalf("env missing %s", PasswordEnv)
}
