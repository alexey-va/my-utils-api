package business

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"time"
)

const tokenDomain = "my-utils-business-bank-token-v1"

type tokenCipher struct{ gcm cipher.AEAD }

func newTokenCipher(secret string) (*tokenCipher, error) {
	if len(secret) < 32 {
		return nil, errors.New("business bank token encryption requires the application JWT secret")
	}
	key := sha256.Sum256([]byte(tokenDomain + "\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &tokenCipher{gcm: gcm}, nil
}
func (c *tokenCipher) encrypt(owner, token string) ([]byte, []byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return c.gcm.Seal(nil, nonce, []byte(token), []byte(tokenDomain+"\x00"+owner)), nonce, nil
}
func (c *tokenCipher) decrypt(owner string, ciphertext, nonce []byte) (string, error) {
	if len(nonce) != c.gcm.NonceSize() {
		return "", unavailable("Не удалось открыть подключение банка")
	}
	plain, err := c.gcm.Open(nil, nonce, ciphertext, []byte(tokenDomain+"\x00"+owner))
	if err != nil {
		return "", unavailable("Не удалось открыть подключение банка")
	}
	return string(plain), nil
}
func parseTimestamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
