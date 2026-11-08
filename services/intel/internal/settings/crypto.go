// Package settings encrypts source credentials saved from the admin UI.
// The encryption key is derived from INTEL_ADMIN_HMAC_SECRET, which the API
// and worker already share, so no extra secret has to be provisioned. If
// that secret rotates, stored credentials no longer decrypt (KeyID differs)
// and the admin is asked to enter them again.
package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

var ErrUnavailable = errors.New("settings encryption key is not configured")
var ErrStale = errors.New("stored credential was encrypted with a different secret")

type Box struct {
	key []byte
	id  string
}

// NewBox derives a dedicated AES-256 key; the admin secret itself is never
// used directly as a cipher key.
func NewBox(adminSecret []byte) (*Box, error) {
	if len(adminSecret) < 32 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, adminSecret)
	mac.Write([]byte("metar-intel/source-credentials/v1"))
	key := mac.Sum(nil)
	sum := sha256.Sum256(key)
	return &Box{key: key, id: hex.EncodeToString(sum[:8])}, nil
}

// KeyID identifies the derived key without revealing it.
func (b *Box) KeyID() string { return b.id }

func (b *Box) Seal(plain string) ([]byte, error) {
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), []byte(b.id)), nil
}

func (b *Box) Open(sealed []byte, keyID string) (string, error) {
	if keyID != b.id {
		return "", ErrStale
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", ErrStale
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(b.id))
	if err != nil {
		return "", ErrStale
	}
	return string(plain), nil
}

// Hint is the last four characters, enough to recognise a key.
func Hint(key string) string {
	if len(key) <= 4 {
		return ""
	}
	return key[len(key)-4:]
}
