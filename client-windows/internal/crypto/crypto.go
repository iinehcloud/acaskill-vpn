// internal/crypto/crypto.go
// ChaCha20-Poly1305 encryption for AcaSkill VPN bonding protocol.
// Zero performance impact — hardware accelerated on x86/ARM.
// Each packet is independently encrypted with a random nonce.

package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	KeySize   = chacha20poly1305.KeySize   // 32 bytes
	NonceSize = chacha20poly1305.NonceSizeX // 24 bytes (XChaCha20)
	Overhead  = chacha20poly1305.Overhead   // 16 bytes auth tag
)

// Session holds the AEAD cipher for a device session.
type Session struct {
	aead interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
		Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
		NonceSize() int
		Overhead() int
	}
}

// NewSession creates a Session from a 32-byte hex session key.
func NewSession(keyHex string) (*Session, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(keyBytes) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeySize, len(keyBytes))
	}
	aead, err := chacha20poly1305.NewX(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	return &Session{aead: aead}, nil
}

// Encrypt encrypts plaintext and returns nonce+ciphertext+tag.
// Output format: [24 nonce][N+16 ciphertext+tag]
func (s *Session) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	out := make([]byte, NonceSize+len(plaintext)+Overhead)
	copy(out[:NonceSize], nonce)
	s.aead.Seal(out[NonceSize:NonceSize], nonce, plaintext, nil)
	return out, nil
}

// Decrypt decrypts nonce+ciphertext+tag and returns plaintext.
func (s *Session) Decrypt(data []byte) ([]byte, error) {
	if len(data) < NonceSize+Overhead {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}
	nonce := data[:NonceSize]
	ciphertext := data[NonceSize:]
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// EncryptedSize returns the encrypted size of a plaintext of given length.
func EncryptedSize(plaintextLen int) int {
	return NonceSize + plaintextLen + Overhead
}

// PlaintextSize returns the plaintext size from an encrypted blob.
func PlaintextSize(encryptedLen int) int {
	return encryptedLen - NonceSize - Overhead
}
