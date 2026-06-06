package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import okhttp3.OkHttpClient
import org.slf4j.LoggerFactory
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.net.InetSocketAddress
import java.net.Proxy
import java.util.concurrent.TimeUnit

@Configuration
@ConditionalOnTelegramBot
class TelegramBotConfiguration {
	private val log = LoggerFactory.getLogger(javaClass)

	@Bean(destroyMethod = "shutdown")
	fun telegramBot(properties: MyUtilsProperties): TelegramBot {
		val proxy = properties.openrouter.proxy
		val httpClient =
			OkHttpClient
				.Builder()
				.connectTimeout(30, TimeUnit.SECONDS)
				.readTimeout(75, TimeUnit.SECONDS)
				.writeTimeout(75, TimeUnit.SECONDS)
				.apply {
					if (proxy.enabled && proxy.host.isNotBlank()) {
						proxy(Proxy(Proxy.Type.HTTP, InetSocketAddress(proxy.host.trim(), proxy.port)))
						log.info("Telegram HTTP via proxy {}:{}", proxy.host, proxy.port)
					}
				}.build()
		return TelegramBot
			.Builder(properties.telegram.botToken)
			.okHttpClient(httpClient)
			.build()
	}
}
