package dev.myutils.api.agent

import com.pengrad.telegrambot.TelegramBot
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.openrouter.ChatMessage
import dev.myutils.api.telegram.TelegramChatHistory
import dev.myutils.api.telegram.sendHtmlMessage
import dev.myutils.api.telegram.sendTyping
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Service

@Service
@ConditionalOnTelegramBot
class WorkoutAgentService(
	private val properties: MyUtilsProperties,
	private val agentLoop: WorkoutAgentLoop,
	private val chatHistory: TelegramChatHistory,
	private val bot: TelegramBot,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val telegram = properties.telegram

	suspend fun handleMessage(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		if (telegram.allowedUserIdSet().isNotEmpty() && userId !in telegram.allowedUserIdSet()) {
			log.warn("Rejected Telegram user {}", userId)
			bot.sendHtmlMessage(chatId, "У вас нет доступа к этому боту.")
			return
		}

		log.info(
			"Telegram inbound chatId={} userId={} text={}",
			chatId,
			userId,
			LogPreview.of(text),
		)

		if (text == "/start") {
			log.info("Telegram /start chatId={}", chatId)
			bot.sendHtmlMessage(
				chatId,
				"""
				Тренер по дневнику. Напиши «что на сегодня» — скажу что уже было на этой неделе, что осталось по списку упражнений, и один план с весами. Или сразу запиши подход: «жим 70 3*10/12».
				""".trimIndent(),
			)
			return
		}

		bot.sendTyping(chatId)

		val history = chatHistory.load(chatId).toMutableList()
		log.info("Telegram chatId={} historyMessages={}", chatId, history.size)
		history.add(ChatMessage(role = "user", content = text))

		val reply = agentLoop.run(chatId, history)
		history.add(ChatMessage(role = "assistant", content = reply))
		chatHistory.save(chatId, history)
		log.info("Telegram chatId={} savedHistoryMessages={}", chatId, history.size)

		bot.sendHtmlMessage(chatId, reply)
		log.info("Telegram handled chatId={} reply={}", chatId, LogPreview.of(reply))
	}
}
