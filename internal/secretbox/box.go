package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// keyVersion is the version of the master key that seals new envelopes.
// Envelopes carry it, so that the master key can be rotated later.
const keyVersion byte = 1

// ErrOpen means that an envelope can't be opened: it was sealed with
// another key or for other data, or it was changed.
var ErrOpen = errors.New("secretbox: the envelope can't be opened")

// Box seals and opens envelopes with AES-256-GCM. Its key is derived from
// the master key for one purpose, such as "cache", so that envelopes of one
// purpose never open as another's.
type Box struct {
	aead cipher.AEAD
}

// New returns the box of masterKey for purpose.
func New(masterKey []byte, purpose string) (*Box, error) {
	if len(masterKey) != KeySize {
		return nil, fmt.Errorf("secretbox: the master key has %d bytes, want %d", len(masterKey), KeySize)
	}
	key, err := hkdf.Key(sha256.New, masterKey, nil, "chowki "+purpose, KeySize)
	if err != nil {
		return nil, fmt.Errorf("secretbox: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext and binds it to data, which Open needs too, so
// that an envelope copied to another record doesn't open there. The
// envelope is the key version, a random nonce, and the ciphertext with its
// authentication tag.
func (b *Box) Seal(plaintext, data []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never fails
	env := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	env = append(append(env, keyVersion), nonce...)
	return b.aead.Seal(env, nonce, plaintext, data)
}

// Open decrypts an envelope that Seal made for the same data, or returns
// ErrOpen.
func (b *Box) Open(envelope, data []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(envelope) < 1+n+b.aead.Overhead() || envelope[0] != keyVersion {
		return nil, ErrOpen
	}
	plaintext, err := b.aead.Open(nil, envelope[1:1+n], envelope[1+n:], data)
	if err != nil {
		return nil, ErrOpen
	}
	return plaintext, nil
}
