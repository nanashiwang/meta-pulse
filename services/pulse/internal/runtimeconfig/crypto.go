package runtimeconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
)

func fingerprint(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// The key never leaves its role's private volume. Atomic linking prevents a
// second process from reading a half-written key during simultaneous startup.
func loadPrivateKey(directory, role string) (*ecdh.PrivateKey, error) {
	if directory == "" || (role != RoleAPI && role != RoleWorker) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, errors.New("create runtime key directory")
	}
	path := filepath.Join(directory, role+".key")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return nil, errors.New("generate runtime encryption key")
		}
		temp, err := os.CreateTemp(directory, ".runtime-key-*")
		if err != nil {
			return nil, errors.New("create runtime key file")
		}
		defer os.Remove(temp.Name())
		_, writeErr := temp.Write(key.Bytes())
		if writeErr == nil {
			writeErr = temp.Sync()
		}
		closeErr := temp.Close()
		if writeErr != nil || closeErr != nil {
			return nil, errors.New("persist runtime key file")
		}
		if err := os.Link(temp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, errors.New("install runtime key file")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return nil, errors.New("runtime key file must be a private regular 32-byte file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read runtime key file")
	}
	key, err := ecdh.X25519().NewPrivateKey(payload)
	if err != nil {
		return nil, errors.New("invalid runtime encryption key")
	}
	return key, nil
}

func envelopeAEAD(shared, ephemeral, recipient []byte, name string) (cipher.AEAD, error) {
	// HKDF binds the secret to both public keys and its exact role/field name.
	info := append([]byte("meta-pulse-runtime-v1:"+name+":"), ephemeral...)
	info = append(info, recipient...)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, nil, info), key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal uses only the recipient's public key, allowing API to save a worker
// secret without possessing any key capable of decrypting it later.
func seal(public []byte, name, value string) ([]byte, error) {
	recipient, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return nil, errors.New("runtime recipient key unavailable")
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.New("generate runtime envelope")
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return nil, errors.New("invalid runtime recipient key")
	}
	aead, err := envelopeAEAD(shared, ephemeral.PublicKey().Bytes(), public, name)
	if err != nil {
		return nil, errors.New("initialize runtime envelope")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("generate runtime envelope nonce")
	}
	result := append([]byte{1}, ephemeral.PublicKey().Bytes()...)
	result = append(result, nonce...)
	return aead.Seal(result, nonce, []byte(value), []byte(name)), nil
}

func unseal(private *ecdh.PrivateKey, name string, payload []byte) (string, error) {
	if len(payload) < 1+32+12+16 || payload[0] != 1 {
		return "", errors.New("invalid runtime secret envelope")
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(payload[1:33])
	if err != nil {
		return "", errors.New("invalid runtime secret envelope")
	}
	shared, err := private.ECDH(ephemeral)
	if err != nil {
		return "", errors.New("invalid runtime secret envelope")
	}
	aead, err := envelopeAEAD(shared, ephemeral.Bytes(), private.PublicKey().Bytes(), name)
	if err != nil {
		return "", errors.New("invalid runtime secret envelope")
	}
	value, err := aead.Open(nil, payload[33:45], payload[45:], []byte(name))
	if err != nil {
		return "", fmt.Errorf("runtime secret %s cannot be decrypted: restore its original role key volume", name)
	}
	return string(value), nil
}
