package dev.myutils.api.wireguard

import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

class WireGuardCredentialsCipher(
	encodedKey: String,
	private val secureRandom: SecureRandom = SecureRandom(),
) {
	private val key = encodedKey.takeIf(String::isNotBlank)?.let { SecretKeySpec(decodeKey(it), "AES") }
	val isConfigured: Boolean
		get() = key != null

	fun encrypt(plaintext: String): EncryptedSecret {
		val nonce = ByteArray(NONCE_SIZE).also(secureRandom::nextBytes)
		val cipher = Cipher.getInstance(TRANSFORMATION)
		cipher.init(Cipher.ENCRYPT_MODE, requireKey(), GCMParameterSpec(TAG_BITS, nonce))
		cipher.updateAAD(AAD)
		val ciphertext = cipher.doFinal(plaintext.toByteArray(Charsets.UTF_8))
		return EncryptedSecret(
			ciphertext = Base64.getEncoder().encodeToString(ciphertext),
			nonce = Base64.getEncoder().encodeToString(nonce),
		)
	}

	fun decrypt(secret: EncryptedSecret): String {
		val nonce = decodeBase64(secret.nonce, "WireGuard credential nonce")
		check(nonce.size == NONCE_SIZE) { "WireGuard credential nonce has an invalid length" }
		val ciphertext = decodeBase64(secret.ciphertext, "WireGuard credential ciphertext")
		val cipher = Cipher.getInstance(TRANSFORMATION)
		cipher.init(Cipher.DECRYPT_MODE, requireKey(), GCMParameterSpec(TAG_BITS, nonce))
		cipher.updateAAD(AAD)
		return cipher.doFinal(ciphertext).toString(Charsets.UTF_8)
	}

	private fun decodeKey(value: String): ByteArray {
		val decoded = decodeBase64(value, "WIREGUARD_CREDENTIALS_ENCRYPTION_KEY")
		check(decoded.size == KEY_SIZE) { "WIREGUARD_CREDENTIALS_ENCRYPTION_KEY must decode to 32 bytes" }
		return decoded
	}

	private fun requireKey(): SecretKeySpec =
		checkNotNull(key) { "WIREGUARD_CREDENTIALS_ENCRYPTION_KEY is not configured" }

	private fun decodeBase64(
		value: String,
		label: String,
	): ByteArray =
		try {
			Base64.getDecoder().decode(value)
		} catch (_: IllegalArgumentException) {
			throw IllegalStateException("$label must be valid base64")
		}

	private companion object {
		const val KEY_SIZE = 32
		const val NONCE_SIZE = 12
		const val TAG_BITS = 128
		const val TRANSFORMATION = "AES/GCM/NoPadding"
		val AAD = "my-utils-wireguard-client-key-v1".toByteArray(Charsets.UTF_8)
	}
}

data class EncryptedSecret(
	val ciphertext: String,
	val nonce: String,
)
