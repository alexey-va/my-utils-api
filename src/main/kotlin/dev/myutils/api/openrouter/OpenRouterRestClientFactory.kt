package dev.myutils.api.openrouter

import dev.myutils.api.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import org.springframework.http.HttpHeaders
import org.springframework.http.MediaType
import org.springframework.http.client.JdkClientHttpRequestFactory
import org.springframework.web.client.RestClient
import java.net.InetSocketAddress
import java.net.ProxySelector
import java.net.http.HttpClient
import java.time.Duration

object OpenRouterRestClientFactory {
	private val log = LoggerFactory.getLogger(OpenRouterRestClientFactory::class.java)

	fun create(properties: MyUtilsProperties): RestClient {
		val config = properties.openrouter
		val builder =
			RestClient
				.builder()
				.baseUrl(config.baseUrl.trimEnd('/'))
				.defaultHeader(HttpHeaders.AUTHORIZATION, "Bearer ${config.apiKey}")
				.defaultHeader(HttpHeaders.CONTENT_TYPE, MediaType.APPLICATION_JSON_VALUE)
				.defaultHeader("HTTP-Referer", config.httpReferer)
				.defaultHeader("X-Title", config.appTitle)

		val proxy = config.proxy
		if (proxy.enabled && proxy.host.isNotBlank()) {
			val address = InetSocketAddress(proxy.host.trim(), proxy.port)
			val httpClient =
				HttpClient
					.newBuilder()
					.proxy(ProxySelector.of(address))
					.connectTimeout(Duration.ofSeconds(30))
					.build()
			builder.requestFactory(JdkClientHttpRequestFactory(httpClient))
			log.info("OpenRouter traffic via HTTP proxy {}:{}", proxy.host, proxy.port)
		}

		return builder.build()
	}
}
