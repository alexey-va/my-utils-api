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
}
