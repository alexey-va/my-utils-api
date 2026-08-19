package wireguard

import (
	"errors"
	"fmt"
	"strings"
)

func RenderClientConfig(privateKey, address, dns, serverPublicKey, endpoint string) (string, error) {
	for _, value := range []string{privateKey, address, dns, serverPublicKey, endpoint} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
			return "", errors.New("WireGuard config value is invalid")
		}
	}
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = %s
MTU = 1280

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, privateKey, address, dns, serverPublicKey, endpoint), nil
}
