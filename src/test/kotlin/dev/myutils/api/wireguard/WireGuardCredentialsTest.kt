package dev.myutils.api.wireguard

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.Base64

class WireGuardCredentialsTest {
	private val encryptionKey = Base64.getEncoder().encodeToString(ByteArray(32) { index -> (index + 1).toByte() })

	@Test
	fun `generates base64 encoded WireGuard X25519 key pairs`() {
		val pair = WireGuardKeyPairGenerator().generate()

		assertEquals(32, Base64.getDecoder().decode(pair.privateKey).size)
		assertEquals(32, Base64.getDecoder().decode(pair.publicKey).size)
		assertNotEquals(pair.privateKey, pair.publicKey)
	}

	@Test
	fun `encrypts with a fresh nonce and restores the private key`() {
		val cipher = WireGuardCredentialsCipher(encryptionKey)
		val plaintext = "uJyk9bAqMJf3NwEwbMlAqvWG1JhO2vG4gFw1yQ4FNEs="

		val first = cipher.encrypt(plaintext)
		val second = cipher.encrypt(plaintext)

		assertNotEquals(plaintext, first.ciphertext)
		assertNotEquals(first.ciphertext, second.ciphertext)
		assertNotEquals(first.nonce, second.nonce)
		assertEquals(plaintext, cipher.decrypt(first))
		assertEquals(plaintext, cipher.decrypt(second))
	}

	@Test
	fun `allows application startup without a key but fails closed on credential use`() {
		val cipher = WireGuardCredentialsCipher("")

		assertFalse(cipher.isConfigured)
		assertThrows(IllegalStateException::class.java) {
			cipher.encrypt("private-key")
		}
	}

	@Test
	fun `rejects a malformed or wrong-sized configured encryption key`() {
		listOf("not-base64", Base64.getEncoder().encodeToString(ByteArray(16))).forEach { value ->
			assertThrows(IllegalStateException::class.java) {
				WireGuardCredentialsCipher(value)
			}
		}
	}

	@Test
	fun `renders an IPv4 WireGuard client config without an IPv6 default route`() {
		val config =
			WireGuardClientConfig.render(
				privateKey = "client-private",
				address = "10.89.0.2",
				dns = "1.1.1.1",
				serverPublicKey = "relay-public",
				endpoint = "vpn.example.net:51820",
			)

		assertEquals(
			"""[Interface]
PrivateKey = client-private
Address = 10.89.0.2/32
DNS = 1.1.1.1
MTU = 1280

[Peer]
PublicKey = relay-public
Endpoint = vpn.example.net:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
""",
			config,
		)
		assertTrue(config.contains("AllowedIPs = 0.0.0.0/0"))
		assertFalse(config.contains("::/0"))
	}
}
