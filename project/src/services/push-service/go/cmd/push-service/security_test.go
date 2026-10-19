package main

import (
	"net"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func TestPushEndpointValidation(t *testing.T) {
	valid := []string{"https://fcm.googleapis.com/send/opaque", "https://push.example.test/v1/abc?token=opaque"}
	for _, endpoint := range valid {
		if err := validatePushEndpoint(endpoint); err != nil {
			t.Errorf("expected endpoint %q to be accepted: %v", endpoint, err)
		}
	}
	invalid := []string{"http://push.example.test/", "https://user:pass@push.example.test/", "https://push.example.test/#fragment", "https://"}
	for _, endpoint := range invalid {
		if err := validatePushEndpoint(endpoint); err == nil {
			t.Errorf("expected endpoint %q to be rejected", endpoint)
		}
	}
}

func TestPublicPushIPBlocksPrivateAndSpecialRanges(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.2", "100.64.0.1", "169.254.1.2", "192.0.2.5", "198.18.0.1", "203.0.113.10", "240.0.0.1", "::1", "fd00::1", "fe80::1", "2001:db8::1"} {
		if isPublicPushIP(net.ParseIP(value)) {
			t.Errorf("expected %s to be blocked", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicPushIP(net.ParseIP(value)) {
			t.Errorf("expected %s to be allowed", value)
		}
	}
}

func TestSecretBoxEncryptsAndRejectsTampering(t *testing.T) {
	box, err := newSecretBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("browser auth secret")
	ciphertext, err := box.seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == string(plain) {
		t.Fatal("encrypted material must differ from plaintext")
	}
	opened, err := box.open(ciphertext)
	if err != nil || string(opened) != string(plain) {
		t.Fatalf("round trip failed: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := box.open(ciphertext); err == nil {
		t.Fatal("tampered ciphertext must not decrypt")
	}
}

func TestVAPIDConfigurationValidatesKeyPairAndSubject(t *testing.T) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateVAPIDKeyPair(public, private); err != nil {
		t.Fatalf("generated VAPID key pair rejected: %v", err)
	}
	if err := validateVAPIDKeyPair(public, public); err == nil {
		t.Fatal("mismatched VAPID key pair must be rejected")
	}
	for _, input := range []string{"ops@example.test", "mailto:ops@example.test", "https://example.test/contact"} {
		if _, err := normalizeVAPIDSubject(input); err != nil {
			t.Errorf("subject %q rejected: %v", input, err)
		}
	}
	if _, err := normalizeVAPIDSubject("javascript:alert(1)"); err == nil {
		t.Fatal("non-contact URI must be rejected")
	}
}
