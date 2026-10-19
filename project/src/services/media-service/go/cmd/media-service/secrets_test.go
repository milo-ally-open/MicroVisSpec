package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMediaSourceEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	source := mediaSourceSecret{URL: "rtsp://10.0.0.8:8554/classroom", Username: "camera-user", Password: "private-source-password"}
	encrypted, err := encryptMediaSource(key, source)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(source.URL)) || bytes.Contains(encrypted, []byte(source.Username)) || bytes.Contains(encrypted, []byte(source.Password)) {
		t.Fatal("encrypted source contains plaintext credentials")
	}
	decrypted, err := decryptMediaSource(key, encrypted)
	if err != nil || decrypted != source {
		t.Fatalf("source did not round-trip: source=%+v err=%v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := decryptMediaSource(key, encrypted); err == nil || strings.Contains(err.Error(), source.Password) {
		t.Fatal("tampered ciphertext should fail without exposing plaintext")
	}
}
