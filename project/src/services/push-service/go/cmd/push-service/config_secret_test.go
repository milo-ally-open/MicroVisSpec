package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValueReadsSecretFile(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "private key", contents: "private-value\n"},
		{name: "public key", contents: "public-value\r\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			key := "PUSH_VAPID_PRIVATE_KEY"
			want := "private-value"
			if test.name == "public key" {
				key, want = "PUSH_VAPID_PUBLIC_KEY", "public-value"
			}
			t.Setenv(key+"_FILE", path)
			value, err := configValue(key)
			if err != nil || value != want {
				t.Fatalf("configValue() = %q, %v; want %q", value, err, want)
			}
		})
	}
}
