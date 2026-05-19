package dev.myutils.api.telegram

import dev.myutils.api.agent.WorkoutAgentService
import dev.myutils.api.config.MyUtilsProperties
import jakarta.annotation.PreDestroy
import org.slf4j.LoggerFactory
import dev.myutils.api.config.ConditionalOnTelegramBot
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/** Receives bot messages via Telegram [getUpdates] long polling. */
@Component
@ConditionalOnTelegramBot
class TelegramLongPollingRunner(
	private val properties: MyUtilsProperties,
	private val telegramClient: TelegramClient,
	private val workoutAgentService: WorkoutAgentService,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val running = AtomicBoolean(false)
	private val nextOffset = AtomicLong(0)
	private var pollingThread: Thread? = null

	@EventListener(ApplicationReadyEvent::class)
	fun start() {
		val allowed = properties.telegram.allowedUserIdSet()
		log.info(
			"Telegram bot starting long polling allowedUsers={} proxy={}",
			allowed.ifEmpty { "any" },
			properties.openrouter.proxy.enabled,
		)
		running.set(true)
		pollingThread =
			Thread.ofVirtual().name("telegram-long-poll").start {
				telegramClient.clearPushUrlIfAny()
				log.info("Telegram long polling active")
				pollLoop()
			}
	}

	@PreDestroy
	fun stop() {
		running.set(false)
		pollingThread?.interrupt()
	}

	private fun pollLoop() {
		while (running.get() && !Thread.currentThread().isInterrupted) {
			try {
				val updates = telegramClient.getUpdates(nextOffset.get(), timeoutSeconds = 30)
				if (updates.isNotEmpty()) {
					log.info("Telegram poll received {} update(s)", updates.size)
				}
				for (update in updates) {
					val updateId = update.updateId
					if (updateId != null) {
						nextOffset.set(updateId + 1)
					}
					workoutAgentService.handleUpdateAsync(update)
				}
			} catch (ex: InterruptedException) {
				Thread.currentThread().interrupt()
				break
			} catch (ex: Exception) {
				log.warn("Telegram polling error: {}", ex.message)
				sleep(3_000)
			}
		}
		log.info("Telegram long polling stopped")
	}

	private fun sleep(ms: Long) {
		try {
			Thread.sleep(ms)
		} catch (_: InterruptedException) {
			Thread.currentThread().interrupt()
		}
	}
}
