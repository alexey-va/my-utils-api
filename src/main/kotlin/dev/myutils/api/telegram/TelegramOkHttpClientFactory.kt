package dev.myutils.api.telegram

import dev.myutils.api.config.MyUtilsProperties
import okhttp3.OkHttpClient
import org.slf4j.LoggerFactory
import java.net.InetSocketAddress
import java.net.Proxy
import java.util.concurrent.TimeUnit

object TelegramOkHttpClientFactory {
	private val log = LoggerFactory.getLogger(TelegramOkHttpClientFactory::class.java)

	fun create(proxy: MyUtilsProperties.OpenRouterProperties.HttpProxyProperties): OkHttpClient {
		val builder =
			OkHttpClient
				.Builder()
				.connectTimeout(30, TimeUnit.SECONDS)
				.readTimeout(75, TimeUnit.SECONDS)
				.writeTimeout(75, TimeUnit.SECONDS)
		if (proxy.enabled && proxy.host.isNotBlank()) {
			val address = InetSocketAddress(proxy.host.trim(), proxy.port)
			builder.proxy(Proxy(Proxy.Type.HTTP, address))
			log.info("Telegram HTTP via proxy {}:{}", proxy.host, proxy.port)
		}
		return builder.build()
	}
}
