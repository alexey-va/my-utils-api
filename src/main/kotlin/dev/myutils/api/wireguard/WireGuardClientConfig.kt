package dev.myutils.api.wireguard

object WireGuardClientConfig {
	fun render(
		privateKey: String,
		address: String,
		dns: String,
		serverPublicKey: String,
		endpoint: String,
	): String {
		listOf(privateKey, address, dns, serverPublicKey, endpoint).forEach { value ->
			require(value.isNotBlank() && '\n' !in value && '\r' !in value) { "WireGuard config value is invalid" }
		}
		return """[Interface]
PrivateKey = $privateKey
Address = $address/32
DNS = $dns
MTU = 1280

[Peer]
PublicKey = $serverPublicKey
Endpoint = $endpoint
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
"""
	}
}
