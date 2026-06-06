package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
@ConditionalOnTelegramBot
class TelegramBotConfiguration {
	@Bean(destroyMethod = "shutdown")
	fun telegramBot(properties: MyUtilsProperties): TelegramBot =
		TelegramBot
			.Builder(properties.telegram.botToken)
			.okHttpClient(TelegramOkHttpClientFactory.create(properties.openrouter.proxy))
			.build()
}
