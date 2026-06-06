package dev.myutils.api.telegram

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.telegram.TelegramApiSupport.describeError
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import org.slf4j.LoggerFactory
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component
import java.util.concurrent.atomic.AtomicLong

/** Receives bot messages via Telegram [getUpdates] long polling. */
@Component
@ConditionalOnTelegramBot
class TelegramLongPollingRunner(
	private val properties: MyUtilsProperties,
	private val telegramClient: TelegramClient,
	private val inboundCoalescer: TelegramInboundCoalescer,
	private val telegramScope: TelegramCoroutineScope,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val nextOffset = AtomicLong(0)
	private var pollingJob: Job? = null

	@EventListener(ApplicationReadyEvent::class)
	fun start() {
		val allowed = properties.telegram.allowedUserIdSet()
		log.info(
			"Telegram bot starting long polling allowedUsers={}",
			allowed.ifEmpty { "any" },
		)
		pollingJob =
			telegramScope.launch {
				try {
					telegramClient.ensureLongPollingMode()
				} catch (ex: CancellationException) {
					throw ex
				} catch (ex: Exception) {
					log.warn("Telegram webhook setup failed: {}", describeError(ex), ex)
				}
				log.info("Telegram long polling active")
				pollLoop()
			}
	}

	@PreDestroy
	fun stop() {
		pollingJob?.cancel()
	}

	private suspend fun pollLoop() {
		while (telegramScope.isActive) {
			try {
				val updates = telegramClient.getUpdates(nextOffset.get(), timeoutSeconds = 30)
				for (update in updates) {
					val updateId = update.updateId
					if (updateId != null) {
						nextOffset.set(updateId + 1)
					}
					val message = update.message ?: update.editedMessage ?: continue
					val userId = message.from?.id ?: continue
					val text = message.text?.trim() ?: continue
					inboundCoalescer.enqueue(message.chat.id, userId, text)
				}
			} catch (ex: CancellationException) {
				throw ex
			} catch (ex: Exception) {
				log.warn("Telegram polling error: {}", describeError(ex), ex)
				delay(POLL_ERROR_BACKOFF_MS)
			}
		}
		log.info("Telegram long polling stopped")
	}

	private companion object {
		const val POLL_ERROR_BACKOFF_MS = 3_000L
	}
}
