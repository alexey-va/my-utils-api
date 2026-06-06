package dev.myutils.api.infra.http

import dev.myutils.api.infra.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import org.springframework.http.client.JdkClientHttpRequestFactory
import java.net.InetSocketAddress
import java.net.ProxySelector
import java.net.http.HttpClient
import java.time.Duration

object OutboundHttpClientFactory {
	private val log = LoggerFactory.getLogger(OutboundHttpClientFactory::class.java)

	fun jdkRequestFactory(proxy: MyUtilsProperties.OpenRouterProperties.HttpProxyProperties): JdkClientHttpRequestFactory {
		val builder =
			HttpClient
				.newBuilder()
				.connectTimeout(Duration.ofSeconds(30))
		if (proxy.enabled && proxy.host.isNotBlank()) {
			val address = InetSocketAddress(proxy.host.trim(), proxy.port)
			builder.proxy(ProxySelector.of(address))
			log.info("Outbound HTTP via proxy {}:{}", proxy.host, proxy.port)
		}
		return JdkClientHttpRequestFactory(builder.build())
	}
}
