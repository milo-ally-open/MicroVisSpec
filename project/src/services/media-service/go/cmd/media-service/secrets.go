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
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func encryptMediaSource(key []byte, source mediaSourceSecret) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptMediaSource(key, encrypted []byte) (mediaSourceSecret, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return mediaSourceSecret{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return mediaSourceSecret{}, err
	}
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return mediaSourceSecret{}, errors.New("encrypted media source is invalid")
	}
	nonce := encrypted[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, encrypted[aead.NonceSize():], nil)
	if err != nil {
		return mediaSourceSecret{}, errors.New("encrypted media source could not be opened")
	}
	var source mediaSourceSecret
	if err := json.Unmarshal(plaintext, &source); err != nil {
		return mediaSourceSecret{}, errors.New("encrypted media source payload is invalid")
	}
	return source, nil
}
