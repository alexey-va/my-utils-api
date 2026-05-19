package dev.myutils.api.config

import org.springframework.boot.context.properties.ConfigurationProperties

@ConfigurationProperties(prefix = "myutils")
data class MyUtilsProperties(
	val jwt: JwtProperties = JwtProperties(),
	val cors: CorsProperties = CorsProperties(),
	val session: SessionProperties = SessionProperties(),
	val telegram: TelegramProperties = TelegramProperties(),
	val openrouter: OpenRouterProperties = OpenRouterProperties(),
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

	data class TelegramProperties(
		val botToken: String = "",
		val webhookSecret: String = "",
		/** Comma-separated Telegram user IDs allowed to use the bot. */
		val allowedUserIds: String = "",
		/** Public API base URL for setWebhook, e.g. https://utils.example.com */
		val webhookBaseUrl: String = "",
		val conversationTtlHours: Long = 48,
		val conversationKeyPrefix: String = "myutils:telegram:chat:",
	) {
		fun allowedUserIdSet(): Set<Long> =
			allowedUserIds
				.split(',')
				.mapNotNull { it.trim().takeIf(String::isNotEmpty)?.toLongOrNull() }
				.toSet()
	}

	data class OpenRouterProperties(
		val apiKey: String = "",
		val model: String = "anthropic/claude-3.5-haiku",
		val baseUrl: String = "https://openrouter.ai/api/v1",
		val maxToolIterations: Int = 8,
		val httpReferer: String = "https://github.com/alexey-va/my-utils",
		val appTitle: String = "my-utils-workout-bot",
		val proxy: HttpProxyProperties = HttpProxyProperties(),
	) {
		data class HttpProxyProperties(
			val enabled: Boolean = false,
			val host: String = "",
			val port: Int = 8888,
		)
	}
}
