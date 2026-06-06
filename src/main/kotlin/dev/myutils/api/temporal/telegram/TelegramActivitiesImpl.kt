package dev.myutils.api.temporal.telegram

import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class TelegramActivitiesImpl(
	private val telegram: TelegramMessenger,
) : TelegramActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun sendMessage(
		chatId: Long,
		text: String,
	) {
		telegram.sendHtmlMessage(chatId, text)
		log.info("Temporal Telegram message sent chatId={} chars={}", chatId, text.length)
	}
}
