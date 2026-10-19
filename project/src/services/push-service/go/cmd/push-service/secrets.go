package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type secretBox struct{ aead cipher.AEAD }

func newSecretBox(key []byte) (*secretBox, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

func (b *secretBox) seal(plaintext []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, errors.New("push encryption is not configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, b.aead.Seal(nil, nonce, plaintext, nil)...), nil
}

func (b *secretBox) open(ciphertext []byte) ([]byte, error) {
	if b == nil || b.aead == nil || len(ciphertext) < b.aead.NonceSize()+b.aead.Overhead() {
		return nil, errors.New("encrypted push key is invalid")
	}
	nonce := ciphertext[:b.aead.NonceSize()]
	return b.aead.Open(nil, nonce, ciphertext[b.aead.NonceSize():], nil)
}
