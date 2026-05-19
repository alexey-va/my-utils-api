package dev.myutils.api.security

import dev.myutils.api.config.MyUtilsProperties
import io.jsonwebtoken.Claims
import io.jsonwebtoken.Jwts
import io.jsonwebtoken.security.Keys
import org.springframework.stereotype.Service
import java.util.Date
import java.util.UUID
import javax.crypto.SecretKey

data class IssuedToken(
	val token: String,
	val sessionId: String,
	val email: String,
)

@Service
class JwtService(
	private val properties: MyUtilsProperties,
) {
	private val key: SecretKey by lazy {
		Keys.hmacShaKeyFor(properties.jwt.secret.toByteArray(Charsets.UTF_8))
	}

	fun issue(email: String): IssuedToken {
		val sessionId = UUID.randomUUID().toString()
		val now = System.currentTimeMillis()
		val expiryMs = properties.jwt.expirationHours * 60 * 60 * 1000
		val token =
			Jwts
				.builder()
				.id(sessionId)
				.subject(email)
				.issuedAt(Date(now))
				.expiration(Date(now + expiryMs))
				.signWith(key)
				.compact()
		return IssuedToken(token = token, sessionId = sessionId, email = email)
	}

	fun parseClaims(token: String): Claims? =
		runCatching {
			Jwts
				.parser()
				.verifyWith(key)
				.build()
				.parseSignedClaims(token)
				.payload
		}.getOrNull()
}
