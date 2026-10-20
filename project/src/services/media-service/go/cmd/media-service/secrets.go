package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
)

type mediaSourceSecret struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
}

const currentMediaSourceVersion = 2

// legacyMediaSourcePayload is used only to remove fields written by the old
// source_username/source_password API. New encryption always serializes the
// URL-only mediaSourceSecret above.
type legacyMediaSourcePayload struct {
	Version  int    `json:"version"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func encryptMediaSource(key []byte, source mediaSourceSecret) ([]byte, error) {
	if source.URL == "" {
		return nil, errors.New("media source URL is empty")
	}
	source.Version = currentMediaSourceVersion
	plaintext, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	return encryptMediaSourcePayload(key, plaintext)
}

func encryptMediaSourcePayload(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptMediaSourcePayload(key, encrypted []byte) (legacyMediaSourcePayload, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return legacyMediaSourcePayload{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return legacyMediaSourcePayload{}, err
	}
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return legacyMediaSourcePayload{}, errors.New("encrypted media source is invalid")
	}
	nonce := encrypted[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, encrypted[aead.NonceSize():], nil)
	if err != nil {
		return legacyMediaSourcePayload{}, errors.New("encrypted media source could not be opened")
	}
	var source legacyMediaSourcePayload
	if err := json.Unmarshal(plaintext, &source); err != nil {
		return legacyMediaSourcePayload{}, errors.New("encrypted media source payload is invalid")
	}
	if source.URL == "" || source.Version > currentMediaSourceVersion {
		return legacyMediaSourcePayload{}, errors.New("encrypted media source version is unsupported")
	}
	return source, nil
}

func decryptMediaSource(key, encrypted []byte) (mediaSourceSecret, error) {
	payload, err := decryptMediaSourcePayload(key, encrypted)
	if err != nil {
		return mediaSourceSecret{}, err
	}
	if payload.Version != currentMediaSourceVersion || payload.Username != "" || payload.Password != "" {
		return mediaSourceSecret{}, errors.New("encrypted media source requires migration")
	}
	return mediaSourceSecret{Version: payload.Version, URL: payload.URL}, nil
}
