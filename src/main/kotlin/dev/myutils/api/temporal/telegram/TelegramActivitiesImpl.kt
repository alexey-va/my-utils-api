package dev.myutils.api.temporal.telegram

import dev.myutils.api.telegram.TelegramClient
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component
import kotlinx.coroutines.runBlocking

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class TelegramActivitiesImpl(
	private val telegramClient: ObjectProvider<TelegramClient>,
) : TelegramActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun sendMessage(
		chatId: Long,
		text: String,
	) {
		val client =
			telegramClient.getIfAvailable()
				?: run {
					log.warn("Telegram send skipped: bot not configured chatId={}", chatId)
					return
				}
		runBlocking {
			client.sendMessage(chatId, text)
		}
		log.info("Temporal Telegram message sent chatId={} chars={}", chatId, text.length)
	}
}
