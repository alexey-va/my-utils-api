package dev.myutils.api.agent

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.openrouter.ChatCompletionRequest
import dev.myutils.api.openrouter.ChatMessage
import dev.myutils.api.openrouter.OpenRouterClient
import dev.myutils.api.openrouter.ToolCall
import dev.myutils.api.telegram.TelegramChatHistory
import dev.myutils.api.telegram.TelegramClient
import dev.myutils.api.telegram.TelegramUpdate
import dev.myutils.api.util.LogPreview
import kotlinx.coroutines.runBlocking
import org.slf4j.LoggerFactory
import org.springframework.scheduling.annotation.Async
import org.springframework.stereotype.Service

@Service
@ConditionalOnTelegramBot
class WorkoutAgentService(
	private val properties: MyUtilsProperties,
	private val openRouterClient: OpenRouterClient,
	private val workoutAgentTools: WorkoutAgentTools,
	private val toolExecutor: WorkoutToolExecutor,
	private val chatHistory: TelegramChatHistory,
	private val telegramClient: TelegramClient,
	private val contextBuilder: WorkoutAgentContextBuilder,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val telegram = properties.telegram

	@Async
	fun handleUpdateAsync(update: TelegramUpdate) {
		log.info("Telegram update queued updateId={}", update.updateId)
		try {
			handleUpdate(update)
		} catch (ex: Exception) {
			log.error("Telegram update failed updateId={}", update.updateId, ex)
		}
	}

	fun handleUpdate(update: TelegramUpdate) {
		val message = update.message ?: update.editedMessage ?: return
		val userId = message.from?.id ?: return
		val chatId = message.chat.id
		val text = message.text?.trim() ?: return
		runBlocking {
			handleMessage(chatId, userId, text)
		}
	}

	suspend fun handleMessage(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		if (telegram.allowedUserIdSet().isNotEmpty() && userId !in telegram.allowedUserIdSet()) {
			log.warn("Rejected Telegram user {}", userId)
			telegramClient.sendMessage(chatId, "У вас нет доступа к этому боту.")
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
			telegramClient.sendMessage(
				chatId,
				"""
				Тренер по дневнику. Напиши «что на сегодня» — скажу что уже было на этой неделе, что осталось по списку упражнений, и один план с весами. Или сразу запиши подход: «жим 70 3*10/12».
				""".trimIndent(),
			)
			return
		}

		telegramClient.sendChatAction(chatId, "typing")

		val history = chatHistory.load(chatId).toMutableList()
		log.info("Telegram chatId={} historyMessages={}", chatId, history.size)
		history.add(ChatMessage(role = "user", content = text))

		val reply = runAgent(chatId, history)
		history.add(ChatMessage(role = "assistant", content = reply))
		chatHistory.save(chatId, history)
		log.info("Telegram chatId={} savedHistoryMessages={}", chatId, history.size)

		telegramClient.sendMessage(chatId, reply)
		log.info("Telegram handled chatId={} reply={}", chatId, LogPreview.of(reply))
	}

	private fun runAgent(
		chatId: Long,
		history: MutableList<ChatMessage>,
	): String {
		val messages = mutableListOf<ChatMessage>()
		messages.add(ChatMessage(role = "system", content = AppProperties.AGENT_SYSTEM_PROMPT.get()))
		messages.add(ChatMessage(role = "system", content = contextBuilder.buildSnapshot()))

		messages.addAll(history)

		val maxIterations = AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get()
		val model = AppProperties.OPENROUTER_MODEL.get()
		for (iteration in 1..maxIterations) {
			log.info(
				"Agent chatId={} iteration={}/{} contextMessages={}",
				chatId,
				iteration,
				maxIterations,
				messages.size,
			)

			val response =
				openRouterClient.chat(
					ChatCompletionRequest(
						model = model,
						messages = messages,
						tools = workoutAgentTools.definitions(),
					),
				)

			val assistant = response.choices.firstOrNull()?.message
			if (assistant == null) {
				log.warn("Agent chatId={} iteration={} empty OpenRouter choice", chatId, iteration)
				return "Не удалось получить ответ от модели. Попробуй ещё раз."
			}

			messages.add(assistant)

			val toolCalls = assistant.toolCalls
			if (toolCalls.isNullOrEmpty()) {
				val reply =
					assistant.content?.trim().takeIf { !it.isNullOrEmpty() }
						?: "Готово."
				log.info(
					"Agent chatId={} finished iteration={} reply={}",
					chatId,
					iteration,
					LogPreview.of(reply),
				)
				return reply
			}

			log.info(
				"Agent chatId={} iteration={} toolCalls={}",
				chatId,
				iteration,
				toolCalls.joinToString { it.function.name },
			)

			var dataChanged = false
			for (call in toolCalls) {
				val result = toolExecutor.execute(call, chatId)
				if (call.mutatesWorkoutData()) {
					dataChanged = true
				}
				messages.add(
					ChatMessage(
						role = "tool",
						content = result,
						toolCallId = call.id,
						name = call.function.name,
					),
				)
			}

			if (dataChanged) {
				refreshSnapshot(messages)
				log.info("Agent chatId={} snapshot refreshed after write", chatId)
			}
		}

		log.warn("Agent chatId={} hit maxToolIterations={}", chatId, maxIterations)
		return "Слишком много шагов. Упрости запрос или разбей на части."
	}

	private fun refreshSnapshot(messages: MutableList<ChatMessage>) {
		messages[SNAPSHOT_MESSAGE_INDEX] =
			ChatMessage(role = "system", content = contextBuilder.buildSnapshot())
	}


	companion object {
		/** Индекс сообщения со снимком: [0] статика, [1] снимок, [2..] история. */
		private const val SNAPSHOT_MESSAGE_INDEX = 1
	}
}

private fun ToolCall.mutatesWorkoutData(): Boolean =
	when (function.name) {
		"log_workout", "delete_workout", "create_exercise", "rename_exercise" -> true
		else -> false
	}
