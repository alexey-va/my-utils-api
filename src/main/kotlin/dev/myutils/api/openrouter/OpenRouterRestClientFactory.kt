package dev.myutils.api.openrouter

import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.http.OutboundHttpClientFactory
import org.springframework.http.HttpHeaders
import org.springframework.http.MediaType
import org.springframework.web.client.RestClient

object OpenRouterRestClientFactory {
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
			builder.requestFactory(OutboundHttpClientFactory.jdkRequestFactory(proxy))
		}

		return builder.build()
	}
}
