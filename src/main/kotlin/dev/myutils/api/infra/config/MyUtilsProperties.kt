package dev.myutils.api.infra.config

import org.springframework.boot.context.properties.ConfigurationProperties

@ConfigurationProperties(prefix = "myutils")
data class MyUtilsProperties(
	val jwt: JwtProperties = JwtProperties(),
	val cors: CorsProperties = CorsProperties(),
	val session: SessionProperties = SessionProperties(),
	val telegram: TelegramProperties = TelegramProperties(),
	val openrouter: OpenRouterProperties = OpenRouterProperties(),
	val temporal: TemporalProperties = TemporalProperties(),
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
		val enabled: Boolean = false,
		val botToken: String = "",
		val pollingEnabled: Boolean = true,
		/** Comma-separated Telegram user IDs allowed to use the bot. */
		val allowedUserIds: String = "",
		val conversationKeyPrefix: String = "myutils:telegram:chat:",
	) {
		fun allowedUserIdSet(): Set<Long> =
			allowedUserIds
				.split(',')
				.mapNotNull { it.trim().takeIf(String::isNotEmpty)?.toLongOrNull() }
				.toSet()
	}

	data class TemporalProperties(
		val enabled: Boolean = false,
		val taskQueue: String = "myutils-main",
	)

	data class OpenRouterProperties(
		val apiKey: String = "",
		val baseUrl: String = "https://openrouter.ai/api/v1",
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
