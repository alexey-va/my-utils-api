package dev.myutils.api.agent

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.openrouter.ChatCompletionRequest
import dev.myutils.api.openrouter.ChatMessage
import dev.myutils.api.openrouter.OpenRouterClient
import dev.myutils.api.openrouter.ToolCall
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class WorkoutAgentLoop(
	private val openRouterClient: OpenRouterClient,
	private val workoutAgentTools: WorkoutAgentTools,
	private val toolExecutor: WorkoutToolExecutor,
	private val contextBuilder: WorkoutAgentContextBuilder,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun run(
		chatId: Long,
		history: List<ChatMessage>,
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
				messages[SNAPSHOT_MESSAGE_INDEX] =
					ChatMessage(role = "system", content = contextBuilder.buildSnapshot())
				log.info("Agent chatId={} snapshot refreshed after write", chatId)
			}
		}

		log.warn("Agent chatId={} hit maxToolIterations={}", chatId, maxIterations)
		return "Слишком много шагов. Упрости запрос или разбей на части."
	}

	private companion object {
		const val SNAPSHOT_MESSAGE_INDEX = 1
	}
}

internal fun ToolCall.mutatesWorkoutData(): Boolean =
	when (function.name) {
		"log_workout", "delete_workout", "create_exercise", "rename_exercise" -> true
		else -> false
	}
