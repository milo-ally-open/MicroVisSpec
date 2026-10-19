package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValueReadsSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-value\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVICE_INTERNAL_SERVICE_TOKEN_FILE", path)
	value, err := configValue("DEVICE_INTERNAL_SERVICE_TOKEN")
	if err != nil || value != "file-value" {
		t.Fatalf("configValue() = %q, %v; want file-value", value, err)
	}
}
