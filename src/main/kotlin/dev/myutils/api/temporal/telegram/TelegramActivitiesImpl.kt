package dev.myutils.api.temporal.telegram

import dev.myutils.api.telegram.AgentStatusMessenger
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
	private val agentStatus: AgentStatusMessenger,
) : TelegramActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun sendMessage(
		chatId: Long,
		text: String,
	) {
		agentStatus.complete(chatId)
		telegram.sendHtmlMessage(chatId, text)
		log.info("Temporal Telegram message sent chatId={} chars={}", chatId, text.length)
	}

	override fun agentStatusThinking(
		chatId: Long,
		step: Int,
	) {
		agentStatus.thinking(chatId, step)
	}

	override fun agentStatusTools(
		chatId: Long,
		toolNames: List<String>,
	) {
		agentStatus.toolsStarted(chatId, toolNames)
	}

	override fun agentStatusComposing(chatId: Long) {
		agentStatus.composingReply(chatId)
	}

	override fun completeAgentStatus(chatId: Long) {
		agentStatus.complete(chatId)
	}

	override fun failAgentStatus(
		chatId: Long,
		text: String,
	) {
		agentStatus.fail(chatId, text)
	}
}
