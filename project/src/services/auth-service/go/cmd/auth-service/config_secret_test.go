package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValueReadsSecretFileBeforeEnvironment(t *testing.T) {
	t.Setenv("AUTH_INTERNAL_SERVICE_TOKEN", "environment-value")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_INTERNAL_SERVICE_TOKEN_FILE", path)

	value, err := configValue("AUTH_INTERNAL_SERVICE_TOKEN")
	if err != nil || value != "file-value" {
		t.Fatalf("configValue() = %q, %v; want file-value", value, err)
	}
}

func TestConfigValueFileErrorsDoNotExposeContents(t *testing.T) {
	secret := "must-not-appear-in-errors"
	path := filepath.Join(t.TempDir(), "missing")
	_ = os.WriteFile(path, []byte(secret), 0o600)
	_ = os.Remove(path)
	t.Setenv("AUTH_INTERNAL_SERVICE_TOKEN_FILE", path)

	_, err := configValue("AUTH_INTERNAL_SERVICE_TOKEN")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("configValue() error must be non-empty and must not expose secret; got %v", err)
	}
}
