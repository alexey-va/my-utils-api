package dev.myutils.api.telegram

import dev.myutils.api.agent.WorkoutAgentService
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.util.LogPreview
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.CoroutineName
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import java.util.concurrent.ConcurrentHashMap

/**
 * Buffers rapid messages per chat and handles them as one combined user turn.
 * Also serializes processing so a chat never runs multiple agent loops in parallel.
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

	private val chats = ConcurrentHashMap<Long, ChatBuffer>()

	fun enqueue(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		launch {
			val buffer = chats.computeIfAbsent(chatId) { ChatBuffer() }
			buffer.mutex.withLock {
				buffer.userId = userId
				buffer.texts.add(text)
				if (buffer.processing) {
					log.debug(
						"Telegram chatId={} message buffered while processing (total={})",
						chatId,
						buffer.texts.size,
					)
					return@launch
				}
				buffer.debounceJob?.cancel()
				buffer.debounceJob =
					launch {
						delay(DEBOUNCE_MS)
						processChat(chatId)
					}
			}
		}
	}

	@PreDestroy
	fun shutdown() {
		job.cancel()
	}

	private suspend fun processChat(chatId: Long) {
		while (true) {
			val batch =
				chats[chatId]?.mutex?.withLock {
					val buffer = chats[chatId] ?: return
					if (buffer.texts.isEmpty()) {
						chats.remove(chatId)
						return
					}
					buffer.processing = true
					buffer.debounceJob = null
					val texts = buffer.texts.toList()
					buffer.texts.clear()
					Batch(buffer.userId, texts)
				} ?: return

			try {
				val combined = batch.texts.joinToString("\n")
				log.info(
					"Telegram coalesced chatId={} parts={} text={}",
					chatId,
					batch.texts.size,
					LogPreview.of(combined),
				)
				workoutAgentService.handleMessage(chatId, batch.userId, combined)
			} catch (ex: Exception) {
				log.error("Telegram coalesced handle failed chatId={}", chatId, ex)
			} finally {
				val continueProcessing =
					chats[chatId]?.mutex?.withLock {
						val buffer = chats[chatId] ?: return@withLock false
						buffer.processing = false
						if (buffer.texts.isEmpty()) {
							chats.remove(chatId)
							false
						} else {
							true
						}
					} ?: false
				if (!continueProcessing) {
					return
				}
			}
		}
	}

	private data class Batch(
		val userId: Long,
		val texts: List<String>,
	)

	private class ChatBuffer(
		val mutex: Mutex = Mutex(),
		var userId: Long = 0,
		val texts: MutableList<String> = mutableListOf(),
		var debounceJob: Job? = null,
		var processing: Boolean = false,
	)

	private companion object {
		const val DEBOUNCE_MS = 1_500L
	}
}
