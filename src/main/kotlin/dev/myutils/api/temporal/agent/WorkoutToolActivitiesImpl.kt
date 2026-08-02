package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.AgentToolMutationPolicy
import dev.myutils.api.agent.ToolArgumentsJsonParser
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.infra.observability.GenAiTracing
import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class WorkoutToolActivitiesImpl(
	private val toolsService: WorkoutToolsService,
	private val objectMapper: ObjectMapper,
	private val genAiTracing: GenAiTracing,
) : WorkoutToolActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun executeTool(input: ToolCallInput): String {
		val toolChatId = input.contextChatId ?: input.chatId
		log.info(
			"Temporal tool {} chatId={} contextChatId={} args={}",
			input.toolName,
			input.chatId,
			toolChatId,
			LogPreview.of(input.argumentsJson, max = 240),
		)
		return genAiTracing.executeTool(
			traceParent = input.traceParent,
			chatId = input.chatId,
			toolName = input.toolName,
			toolCallId = input.toolCallId,
			argumentsJson = input.argumentsJson,
		) {
			try {
				AgentToolMutationPolicy.denialReason(input.toolName, input.userMessage)?.let { reason ->
					log.warn(
						"Temporal tool mutation denied tool={} chatId={} userMessage={}",
						input.toolName,
						input.chatId,
						LogPreview.of(input.userMessage.orEmpty(), max = 160),
					)
					return@executeTool ToolExecutionFeedback.failure(
						error = reason,
						hint = "Ответь на вопрос из снимка без изменения данных.",
					)
				}
				when (val parsed = ToolArgumentsJsonParser.parse(objectMapper, input.argumentsJson)) {
					is ToolArgumentsJsonParser.ParseResult.Ok ->
						toolsService.runTool(
							input.toolName,
							toolChatId,
							parsed.args,
							input.publishStatus,
						)
					is ToolArgumentsJsonParser.ParseResult.Error ->
						ToolExecutionFeedback.failure(
							error = parsed.message,
							hint = "Исправь arguments JSON и вызови инструмент снова. Даты — строки YYYY-MM-DD в кавычках.",
						)
				}
			} catch (ex: Exception) {
				log.warn(
					"Temporal tool {} failed chatId={}: {}",
					input.toolName,
					input.chatId,
					ex.message,
					ex,
				)
				ToolExecutionFeedback.failure(
					error = "Ошибка инструмента ${input.toolName}: ${ex.message ?: "неизвестная ошибка"}",
				)
			}
		}
	}
}
