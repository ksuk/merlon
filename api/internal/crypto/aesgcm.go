package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// Encryptor implements field-level AES-256-GCM encryption. Ciphertext
// format: [1 byte key_version][12 byte nonce][ciphertext+GCM tag], base64
// encoded (security.md §2.1).
type Encryptor struct {
	keyRing *KeyRing
}

func NewEncryptor(keyRing *KeyRing) *Encryptor {
	return &Encryptor{keyRing: keyRing}
}

func (e *Encryptor) gcmForVersion(version uint8) (cipher.AEAD, error) {
	key, err := e.keyRing.Key(version)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt always uses the KeyRing's current key version.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	version := e.keyRing.CurrentVersion()
	gcm, err := e.gcmForVersion(version)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	out := make([]byte, 0, 1+len(nonce)+len(ciphertext))
	out = append(out, version)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt selects the decryption key by the key_version embedded in
// ciphertext's first byte, so data encrypted under a retired-but-still-held
// key remains readable after rotation.
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("crypto: invalid base64 ciphertext: %w", err)
	}
	if len(raw) < 1 {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}

	version := raw[0]
	gcm, err := e.gcmForVersion(version)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < 1+nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}
	nonce := raw[1 : 1+nonceSize]
	sealed := raw[1+nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}
