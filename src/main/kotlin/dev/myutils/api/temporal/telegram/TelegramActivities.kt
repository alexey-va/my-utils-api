package dev.myutils.api.temporal.telegram

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface TelegramActivities {
	@ActivityMethod
	fun sendMessage(
		chatId: Long,
		text: String,
	)

	@ActivityMethod
	fun agentStatusThinking(
		chatId: Long,
		step: Int,
	)

	@ActivityMethod
	fun agentStatusTools(
		chatId: Long,
		toolNames: List<String>,
	)

	@ActivityMethod
	fun agentStatusToolsDone(chatId: Long)

	@ActivityMethod
	fun agentStatusComposing(chatId: Long)

	@ActivityMethod
	fun completeAgentStatus(chatId: Long)

	@ActivityMethod
	fun failAgentStatus(
		chatId: Long,
		text: String,
	)
}
