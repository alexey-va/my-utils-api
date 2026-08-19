package wireguard

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

var credentialAAD = []byte("my-utils-wireguard-client-key-v1")

type EncryptedSecret struct {
	Ciphertext string
	Nonce      string
}

type CredentialsCipher struct {
	gcm cipher.AEAD
}

func NewCredentialsCipher(encodedKey string) (*CredentialsCipher, error) {
	if strings.TrimSpace(encodedKey) == "" {
		return &CredentialsCipher{}, nil
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, errors.New("WIREGUARD_CREDENTIALS_ENCRYPTION_KEY must be valid base64")
	}
	if len(key) != 32 {
		return nil, errors.New("WIREGUARD_CREDENTIALS_ENCRYPTION_KEY must decode to 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &CredentialsCipher{gcm: gcm}, nil
}

func (c *CredentialsCipher) Configured() bool { return c != nil && c.gcm != nil }

func (c *CredentialsCipher) Encrypt(plaintext string) (EncryptedSecret, error) {
	if !c.Configured() {
		return EncryptedSecret{}, errors.New("WIREGUARD_CREDENTIALS_ENCRYPTION_KEY is not configured")
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedSecret{}, err
	}
	ciphertext := c.gcm.Seal(nil, nonce, []byte(plaintext), credentialAAD)
	return EncryptedSecret{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
	}, nil
}

func (c *CredentialsCipher) Decrypt(secret EncryptedSecret) (string, error) {
	if !c.Configured() {
		return "", errors.New("WIREGUARD_CREDENTIALS_ENCRYPTION_KEY is not configured")
	}
	nonce, err := base64.StdEncoding.DecodeString(secret.Nonce)
	if err != nil || len(nonce) != c.gcm.NonceSize() {
		return "", errors.New("WireGuard credential nonce has an invalid encoding or length")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return "", errors.New("WireGuard credential ciphertext must be valid base64")
	}
	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, credentialAAD)
	if err != nil {
		return "", fmt.Errorf("decrypt WireGuard credential: %w", err)
	}
	return string(plaintext), nil
}

type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

func GenerateKeyPair() (KeyPair, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		PrivateKey: base64.StdEncoding.EncodeToString(private.Bytes()),
		PublicKey:  base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()),
	}, nil
}
