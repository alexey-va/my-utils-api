package dev.myutils.api.agent

import com.pengrad.telegrambot.TelegramBot
import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.temporal.TemporalWorkflowService
import dev.myutils.api.temporal.agent.AgentTurnInput
import dev.myutils.api.telegram.sendHtmlMessage
import dev.myutils.api.telegram.sendTyping
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.stereotype.Service

@Service
@ConditionalOnTelegramBot
class WorkoutAgentService(
	private val properties: MyUtilsProperties,
	private val langChain4jAgent: WorkoutLangChain4jAgent,
	private val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
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

		val temporal = temporalWorkflow.getIfAvailable()
		if (temporal != null && properties.temporal.enabled) {
			if (text != "/start") {
				bot.sendTyping(chatId)
			}
			temporal.startAgentTurn(
				AgentTurnInput(
					chatId = chatId,
					userId = userId,
					text = text,
				),
			)
			log.info("Telegram chatId={} delegated to Temporal agent workflow", chatId)
			return
		}

		runDirect(chatId, userId, text)
	}

	private suspend fun runDirect(
		chatId: Long,
		userId: Long,
		text: String,
	) {
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
		val reply = langChain4jAgent.run(chatId, text)
		bot.sendHtmlMessage(chatId, reply)
		log.info("Telegram handled chatId={} reply={}", chatId, LogPreview.of(reply))
	}
}
