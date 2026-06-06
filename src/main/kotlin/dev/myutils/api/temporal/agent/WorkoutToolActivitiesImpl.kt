package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.WorkoutToolsService
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
) : WorkoutToolActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun executeTool(input: ToolCallInput): String {
		log.info(
			"Temporal tool {} chatId={} args={}",
			input.toolName,
			input.chatId,
			LogPreview.of(input.argumentsJson, max = 240),
		)
		val args = parseArguments(input.argumentsJson)
		return toolsService.runTool(input.toolName, input.chatId, args)
	}

	private fun parseArguments(argumentsJson: String): Map<String, String?> {
		if (argumentsJson.isBlank()) {
			return emptyMap()
		}
		val node = objectMapper.readTree(argumentsJson)
		if (!node.isObject) {
			return emptyMap()
		}
		return node
			.properties()
			.associate { (key, value) ->
				key to
					when {
						value.isNull -> null
						value.isTextual -> value.asText()
						else -> value.toString()
					}
			}
	}
}
