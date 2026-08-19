package wireguard

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCredentialsAndKeyPairCompatibility(t *testing.T) {
	t.Parallel()

	keyBytes := make([]byte, 32)
	for index := range keyBytes {
		keyBytes[index] = byte(index + 1)
	}
	cipher, err := NewCredentialsCipher(base64.StdEncoding.EncodeToString(keyBytes))
	if err != nil {
		t.Fatalf("NewCredentialsCipher() error = %v", err)
	}
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	private, privateErr := base64.StdEncoding.DecodeString(pair.PrivateKey)
	public, publicErr := base64.StdEncoding.DecodeString(pair.PublicKey)
	if privateErr != nil || publicErr != nil || len(private) != 32 || len(public) != 32 || pair.PrivateKey == pair.PublicKey {
		t.Fatalf("invalid key pair: %#v", pair)
	}
	encrypted, err := cipher.Encrypt(pair.PrivateKey)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	restored, err := cipher.Decrypt(encrypted)
	if err != nil || restored != pair.PrivateKey {
		t.Fatalf("Decrypt() = %q, %v", restored, err)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(encrypted.Nonce); len(decoded) != 12 {
		t.Fatalf("nonce length = %d", len(decoded))
	}
}

func TestCIDRAndClientConfigContract(t *testing.T) {
	t.Parallel()

	cidr, err := ParseCIDR("10.89.0.41/24")
	if err != nil {
		t.Fatalf("ParseCIDR() error = %v", err)
	}
	if cidr.String() != "10.89.0.0/24" || cidr.LastUsableHostOffset() != 254 {
		t.Fatalf("CIDR = %s, last=%d", cidr.String(), cidr.LastUsableHostOffset())
	}
	if got, err := cidr.HostAddress(2); err != nil || got != "10.89.0.2" {
		t.Fatalf("HostAddress(2) = %q, %v", got, err)
	}
	config, err := RenderClientConfig("client-private", "10.89.0.2", "1.1.1.1", "relay-public", "vpn.example.net:51820")
	if err != nil {
		t.Fatalf("RenderClientConfig() error = %v", err)
	}
	if !strings.Contains(config, "AllowedIPs = 0.0.0.0/0") || strings.Contains(config, "::/0") || !strings.HasSuffix(config, "PersistentKeepalive = 25\n") {
		t.Fatalf("client config = %q", config)
	}
}
