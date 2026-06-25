package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.databind.ObjectMapper
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
		log.info(
			"Temporal tool {} chatId={} args={}",
			input.toolName,
			input.chatId,
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
				when (val parsed = ToolArgumentsJsonParser.parse(objectMapper, input.argumentsJson)) {
					is ToolArgumentsJsonParser.ParseResult.Ok ->
						toolsService.runTool(input.toolName, input.chatId, parsed.args)
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
