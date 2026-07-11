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
	fun updateAgentStatus(
		chatId: Long,
		text: String,
	)

	@ActivityMethod
	fun completeAgentStatus(chatId: Long)

	@ActivityMethod
	fun failAgentStatus(
		chatId: Long,
		text: String,
	)
}
