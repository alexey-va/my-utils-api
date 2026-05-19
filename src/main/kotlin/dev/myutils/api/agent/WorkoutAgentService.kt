package dev.myutils.api.agent

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.openrouter.ChatCompletionRequest
import dev.myutils.api.openrouter.ChatMessage
import dev.myutils.api.openrouter.OpenRouterClient
import dev.myutils.api.openrouter.ToolCall
import dev.myutils.api.telegram.TelegramChatHistory
import dev.myutils.api.telegram.TelegramClient
import dev.myutils.api.telegram.TelegramUpdate
import dev.myutils.api.util.LogPreview
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
	private val config = properties.openrouter
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
		messages.add(ChatMessage(role = "system", content = staticSystemPrompt()))
		messages.add(ChatMessage(role = "system", content = contextBuilder.buildSnapshot()))

		messages.addAll(history)

		for (iteration in 1..config.maxToolIterations) {
			log.info(
				"Agent chatId={} iteration={}/{} contextMessages={}",
				chatId,
				iteration,
				config.maxToolIterations,
				messages.size,
			)

			val response =
				openRouterClient.chat(
					ChatCompletionRequest(
						model = config.model,
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

		log.warn("Agent chatId={} hit maxToolIterations={}", chatId, config.maxToolIterations)
		return "Слишком много шагов. Упрости запрос или разбей на части."
	}

	private fun refreshSnapshot(messages: MutableList<ChatMessage>) {
		messages[SNAPSHOT_MESSAGE_INDEX] =
			ChatMessage(role = "system", content = contextBuilder.buildSnapshot())
	}

	private fun staticSystemPrompt(): String =
		"""
		Ты персональный тренер по силовому дневнику. Русский. Тон: уверенный, короткий, как тренер в зале — без воды.

		## Главное
		Всегда давай ОДНО конкретное действие или план. Запрещено: «можно», «попробуй», «варианты», меню из упражнений, «зависит от самочувствия» без цифр.

		## План на сегодня (привет, «что делать», начало тренировки)
		Используй 2-е system-сообщение (снимок). Структура ответа:
		1) **На этой неделе уже:** 2–5 пунктов из раздела «Эта неделя — уже сделано» (если пусто — так и скажи).
		2) **Осталось по дневнику:** из «ещё не делали» + группы с пробелами в «Баланс групп мышц» (приоритет — не трогали на неделе).
		3) **Сегодня делаем:** 2–4 упражнения с весами в формате «Название: W кг 3*X/МАХ», чередуя группы мышц (не трижды одна группа). Веса — из «Все упражнения (последняя сессия)» + прогрессия ниже.
		Не подменяй недельный обзор списком «последних весов без дат» — только факты из снимка.

		## Прогрессия (применяй сам, не переспрашивай)
		- Нет записи по упражнению → пустой гриф или минимальный вес, 3*8/МАХ (новичку — тренажёр).
		- Последний МАХ ≥ 10–12 на текущем весе → следующий раз +2.5 кг, цель X=8–10, МАХ снова добить.
		- МАХ < 8 или провал → тот же вес, цель X=8–10.
		- Долго не тренировал (>10 дней) → −2.5…5 кг от последнего или тот же вес осторожно.
		Округляй вес штанги шагом 2.5 кг. Гантели — вес одной, в названии «гантели».

		## Формат записи
		вес 3*X/МАХ → log_workout(set_count=3, reps_per_set=X, max_reps=МАХ). weight_kg — полный вес (гриф+блины). Смит: гриф ~8 кг.

		## Данные
		2-е system — снимок дневника. Сначала он; tools только если данных нет.
		log_workout / delete_workout / create_exercise / rename_exercise — для изменений. После записи/удаления/переименования — 2–4 строки: что сделано + «Сейчас в дневнике:» из обновлённого снимка.
		send_notification / schedule_notification / cancel_notification — уведомления в этот чат через Temporal (если доступны). Сохраняй workflow_id из schedule для отмены.

		## Формат ответа (пример)
		«На неделе уже: присед 50, бицепс 40. Осталось: жим, бабочка, пулл. Сегодня: Жим грудь 70 кг 3*10/МАХ, Бабочка 35 кг 3*10/МАХ, Пулл даун 72 кг 3*8/МАХ — спина после груди.»
		Не выдумывай цифры. Если упражнение неясно — один короткий вопрос.
		""".trimIndent()

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
