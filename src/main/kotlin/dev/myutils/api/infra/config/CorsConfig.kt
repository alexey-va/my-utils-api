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
		val config = CorsConfiguration().apply {
			allowedOrigins = properties.cors.allowedOrigins
			allowedMethods = listOf("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
			allowedHeaders = listOf("*")
			allowCredentials = true
			maxAge = 3600
		}
		return UrlBasedCorsConfigurationSource().apply {
			registerCorsConfiguration("/api/**", config)
		}
	}
}
