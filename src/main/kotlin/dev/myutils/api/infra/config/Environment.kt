package dev.myutils.api.infra.config

/**
 * Рантайм-окружение приложения.
 *
 * - [PRODUCTION] — реальные outbound-клиенты (OpenRouter, Telegram, Redis memory)
 * - [TESTING] — in-memory фейки вместо HTTP (Spring profile [SPRING_PROFILE])
 */
enum class Environment {
	PRODUCTION,
	TESTING,
	;

	val usesFakeClients: Boolean
		get() = this == TESTING

	companion object {
		const val SPRING_PROFILE = "testing"
		const val PROPERTY = "myutils.environment"

		fun resolve(annotated: Environment? = null): Environment =
			System
				.getProperty(PROPERTY)
				?.let { raw -> entries.find { it.name.equals(raw, ignoreCase = true) } }
				?: annotated
				?: PRODUCTION
	}
}
