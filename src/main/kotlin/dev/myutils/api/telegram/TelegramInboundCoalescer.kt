package dev.myutils.api.telegram

import dev.myutils.api.agent.WorkoutAgentService
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.infra.util.PerKeyLatestBuffer
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.CoroutineName
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class TelegramInboundCoalescer(
	private val workoutAgentService: WorkoutAgentService,
) : CoroutineScope {
	private val log = LoggerFactory.getLogger(javaClass)
	private val job = SupervisorJob()
	override val coroutineContext =
		job + Dispatchers.Default + CoroutineName("telegram-agent")

	private val buffer =
		PerKeyLatestBuffer(
			scope = this,
			log = log,
			keyLabel = { chatId -> "Telegram chatId=$chatId" },
		) { chatId: Long, inbound: Inbound ->
			log.info(
				"Telegram handling chatId={} text={}",
				chatId,
				LogPreview.of(inbound.text),
			)
			try {
				workoutAgentService.handleMessage(chatId, inbound.userId, inbound.text)
			} catch (ex: Exception) {
				log.error("Telegram handle failed chatId={}", chatId, ex)
			}
		}

	fun enqueue(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		buffer.submit(chatId, Inbound(userId, text))
	}

	@PreDestroy
	fun shutdown() {
		job.cancel()
	}

	private data class Inbound(
		val userId: Long,
		val text: String,
	)
}
