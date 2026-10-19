package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValueReadsSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUSH_SERVICE_INTERNAL_TOKEN_FILE", path)
	value, err := configValue("PUSH_SERVICE_INTERNAL_TOKEN")
	if err != nil || value != "file-value" {
		t.Fatalf("configValue() = %q, %v; want file-value", value, err)
	}
}
