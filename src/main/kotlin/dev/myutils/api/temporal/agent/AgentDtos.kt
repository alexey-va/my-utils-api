package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.annotation.JsonIgnore

/** Результат проверки доступа /start перед основным циклом агента. */
data class AgentPreludeResult(
	val kind: Kind,
	val message: String? = null,
) {
	enum class Kind {
		CONTINUE,
		REPLY,
	}
}

data class AgentLlmStepInput(
	val chatId: Long,
	val userMessage: String? = null,
	val traceParent: String? = null,
)

data class ToolCallDto(
	val id: String,
	val name: String,
	val argumentsJson: String,
)

data class AgentLlmStepResult(
	val reply: String = "",
	val toolCalls: List<ToolCallDto> = emptyList(),
) {
	@get:JsonIgnore
	val hasToolCalls: Boolean
		get() = toolCalls.isNotEmpty()
}

data class ToolCallResultDto(
	val toolCallId: String,
	val toolName: String,
	val result: String,
)

data class RecordToolResultsInput(
	val chatId: Long,
	val results: List<ToolCallResultDto>,
)

data class ToolCallInput(
	val chatId: Long,
	val toolName: String,
	val argumentsJson: String,
	val traceParent: String? = null,
	val toolCallId: String? = null,
)
