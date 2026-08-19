package dev.myutils.api.infra.config

import org.springframework.boot.context.properties.EnableConfigurationProperties
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import dev.myutils.api.wireguard.WireGuardCredentialsCipher

@Configuration
@EnableConfigurationProperties(MyUtilsProperties::class)
class AppConfig {
	@Bean
	fun wireGuardCredentialsCipher(properties: MyUtilsProperties): WireGuardCredentialsCipher =
		WireGuardCredentialsCipher(properties.wireguard.credentialsEncryptionKey)
}
