package dev.myutils.api.telegram

import dev.myutils.api.config.ConditionalOnTelegramBot
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.CoroutineName
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class TelegramCoroutineScope :
	CoroutineScope by CoroutineScope(
		SupervisorJob() +
			Dispatchers.IO +
			CoroutineName("telegram"),
	) {
	@PreDestroy
	fun shutdown() {
		cancel()
	}
}
