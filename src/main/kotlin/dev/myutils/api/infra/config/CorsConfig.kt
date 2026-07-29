package dev.myutils.api.infra.config

import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.web.cors.CorsConfiguration
import org.springframework.web.cors.CorsConfigurationSource
import org.springframework.web.cors.UrlBasedCorsConfigurationSource

@Configuration
class CorsConfig(
	private val properties: MyUtilsProperties,
) {
	@Bean
	fun corsConfigurationSource(): CorsConfigurationSource {
		val defaultConfig = CorsConfiguration().apply {
			allowedOrigins = properties.cors.allowedOrigins
			allowedMethods = listOf("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
			allowedHeaders = listOf("*")
			allowCredentials = true
			maxAge = 3600
		}
		val clientEventsConfig = CorsConfiguration().apply {
			allowedOrigins =
				(properties.cors.allowedOrigins + ROUTE_PLANNER_ORIGIN + MY_UTILS_ORIGIN)
					.distinct()
			allowedMethods = listOf("POST", "OPTIONS")
			allowedHeaders = listOf("Content-Type")
			allowCredentials = false
			maxAge = 3600
		}
		return UrlBasedCorsConfigurationSource().apply {
			registerCorsConfiguration("/api/client-events", clientEventsConfig)
			registerCorsConfiguration("/api/**", defaultConfig)
		}
	}

	private companion object {
		const val ROUTE_PLANNER_ORIGIN = "https://route.alexeyav.ru"
		const val MY_UTILS_ORIGIN = "https://utils.alexeyav.ru"
	}
}
