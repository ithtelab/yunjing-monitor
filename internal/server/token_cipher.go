package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedTokenPrefix = "enc:v1:"

type tokenCipher struct {
	aead cipher.AEAD
}

func newTokenCipher(secret string) (*tokenCipher, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("token encryption secret is required")
	}
	key := sha256.Sum256([]byte("monitor-party-agent-token-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &tokenCipher{aead: aead}, nil
}

func (c *tokenCipher) encrypt(plaintext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("token encryption is not configured")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate token nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return encryptedTokenPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *tokenCipher) decrypt(value string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("token encryption is not configured")
	}
	if !strings.HasPrefix(value, encryptedTokenPrefix) {
		return "", errors.New("token is not encrypted")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, encryptedTokenPrefix))
	if err != nil {
		return "", errors.New("invalid encrypted token encoding")
	}
	if len(payload) < c.aead.NonceSize()+c.aead.Overhead() {
		return "", errors.New("invalid encrypted token length")
	}
	nonce, ciphertext := payload[:c.aead.NonceSize()], payload[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("encrypted token cannot be decrypted")
	}
	return string(plaintext), nil
}

func (c *tokenCipher) encryptStoredValue(value string) (string, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, encryptedTokenPrefix) {
		return value, false, nil
	}
	encrypted, err := c.encrypt(value)
	return encrypted, err == nil, err
}
