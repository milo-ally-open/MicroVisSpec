package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMediaSourceEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	source := mediaSourceSecret{URL: "rtsp://10.0.0.8:8554/classroom"}
	encrypted, err := encryptMediaSource(key, source)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(source.URL)) {
		t.Fatal("encrypted source contains the plaintext URL")
	}
	decrypted, err := decryptMediaSource(key, encrypted)
	source.Version = currentMediaSourceVersion
	if err != nil || decrypted != source {
		t.Fatalf("source did not round-trip: source=%+v err=%v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := decryptMediaSource(key, encrypted); err == nil || strings.Contains(err.Error(), source.URL) {
		t.Fatal("tampered ciphertext should fail without exposing plaintext")
	}
}
