package dev.myutils.api.config

import org.springframework.boot.context.properties.ConfigurationProperties

@ConfigurationProperties(prefix = "myutils")
data class MyUtilsProperties(
	val jwt: JwtProperties = JwtProperties(),
	val cors: CorsProperties = CorsProperties(),
	val session: SessionProperties = SessionProperties(),
) {
	data class JwtProperties(
		val secret: String = "dev-secret",
		val expirationHours: Long = 24,
	)

	data class CorsProperties(
		val allowedOrigins: List<String> = listOf("http://localhost:5173"),
	)

	data class SessionProperties(
		val redisKeyPrefix: String = "myutils:session:",
	)
}
