package dev.myutils.api.wireguard

import org.bouncycastle.math.ec.rfc7748.X25519
import org.springframework.stereotype.Component
import java.security.SecureRandom
import java.util.Base64

@Component
class WireGuardKeyPairGenerator(
	private val secureRandom: SecureRandom = SecureRandom(),
) {
	fun generate(): WireGuardKeyPair {
		val privateKey = ByteArray(X25519.SCALAR_SIZE)
		secureRandom.nextBytes(privateKey)
		privateKey[0] = (privateKey[0].toInt() and 248).toByte()
		privateKey[31] = (privateKey[31].toInt() and 127 or 64).toByte()

		val publicKey = ByteArray(X25519.POINT_SIZE)
		X25519.generatePublicKey(privateKey, 0, publicKey, 0)
		return WireGuardKeyPair(
			privateKey = Base64.getEncoder().encodeToString(privateKey),
			publicKey = Base64.getEncoder().encodeToString(publicKey),
		)
	}
}

data class WireGuardKeyPair(
	val privateKey: String,
	val publicKey: String,
)
