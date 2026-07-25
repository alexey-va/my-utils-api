package dev.myutils.api.infra.security

import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.domain.User
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
	val userId: String,
)

@Service
class JwtService(
	private val properties: MyUtilsProperties,
) {
	private val key: SecretKey by lazy {
		Keys.hmacShaKeyFor(properties.jwt.secret.toByteArray(Charsets.UTF_8))
	}

	fun issue(user: User): IssuedToken {
		val sessionId = UUID.randomUUID().toString()
		val now = System.currentTimeMillis()
		val expiryMs = properties.jwt.expirationHours * 60 * 60 * 1000
		val token =
			Jwts
				.builder()
				.id(sessionId)
				.subject(user.id.toString())
				.claim("username", user.username)
				.claim("role", user.role.name)
				.issuedAt(Date(now))
				.expiration(Date(now + expiryMs))
				.signWith(key)
				.compact()
		return IssuedToken(token = token, sessionId = sessionId, userId = user.id.toString())
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
