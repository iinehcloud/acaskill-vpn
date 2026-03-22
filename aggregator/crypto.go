package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	CryptoNonceSize = chacha20poly1305.NonceSizeX
	CryptoOverhead  = chacha20poly1305.Overhead
)

type CryptoSession struct {
	key []byte
}

func newCryptoSession(keyHex string) (*CryptoSession, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("key must be 32 bytes")
	}
	return &CryptoSession{key: key}, nil
}

func (s *CryptoSession) decrypt(data []byte) ([]byte, error) {
	if len(data) < CryptoNonceSize+CryptoOverhead {
		return nil, fmt.Errorf("data too short: %d", len(data))
	}
	aead, err := chacha20poly1305.NewX(s.key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, data[:CryptoNonceSize], data[CryptoNonceSize:], nil)
}

func (s *CryptoSession) encrypt(plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(s.key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, CryptoNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, CryptoNonceSize, CryptoNonceSize+len(plaintext)+CryptoOverhead)
	copy(out, nonce)
	return aead.Seal(out, nonce, plaintext, nil), nil
}
