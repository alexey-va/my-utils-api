package dev.myutils.api.telegram

import dev.myutils.api.agent.WorkoutAgentService
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.util.LogPreview
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.CoroutineName
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import java.util.concurrent.ConcurrentHashMap

/**
 * One agent turn per chat at a time. While busy or before drain starts, only the latest
 * inbound text is kept — older messages are dropped (covers offline backlog bursts).
 */
@Component
@ConditionalOnTelegramBot
class TelegramInboundCoalescer(
	private val workoutAgentService: WorkoutAgentService,
) : CoroutineScope {
	private val log = LoggerFactory.getLogger(javaClass)
	private val job = SupervisorJob()
	override val coroutineContext =
		job + Dispatchers.Default + CoroutineName("telegram-agent")

	private val chats = ConcurrentHashMap<Long, ChatState>()

	fun enqueue(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		val state = chats.computeIfAbsent(chatId) { ChatState() }
		val startDrain =
			synchronized(state) {
				val replaced = state.latest != null
				state.latest = Inbound(userId, text)
				if (replaced) {
					log.debug("Telegram chatId={} replaced pending message with newer one", chatId)
				}
				if (state.draining) {
					false
				} else {
					state.draining = true
					true
				}
			}
		if (startDrain) {
			launch { drainChat(chatId, state) }
		}
	}

	@PreDestroy
	fun shutdown() {
		job.cancel()
	}

	private suspend fun drainChat(
		chatId: Long,
		state: ChatState,
	) {
		while (true) {
			val inbound =
				synchronized(state) {
					val next = state.latest
					state.latest = null
					if (next == null) {
						state.draining = false
						chats.remove(chatId, state)
						null
					} else {
						next
					}
				} ?: return

			try {
				log.info(
					"Telegram handling chatId={} text={}",
					chatId,
					LogPreview.of(inbound.text),
				)
				workoutAgentService.handleMessage(chatId, inbound.userId, inbound.text)
			} catch (ex: Exception) {
				log.error("Telegram handle failed chatId={}", chatId, ex)
			}
		}
	}

	private data class Inbound(
		val userId: Long,
		val text: String,
	)

	private class ChatState {
		var latest: Inbound? = null
		var draining: Boolean = false
	}
}
