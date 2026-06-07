package dev.myutils.api.infra.util

import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage

object LlmRequestLog {
	fun summarize(messages: List<ChatMessage>): List<String> = messages.map { summarizeOne(it) }

	fun summarizeOne(message: ChatMessage): String =
		when (message) {
			is SystemMessage ->
				"system: ${LogPreview.of(message.text(), MAX_CONTENT)}"
			is UserMessage ->
				"user: ${LogPreview.of(message.singleText(), MAX_CONTENT)}"
			is AiMessage -> summarizeAi(message)
			is ToolExecutionResultMessage ->
				"tool:${message.toolName()}(${message.id()}): ${LogPreview.of(message.text(), MAX_CONTENT)}"
			else ->
				"${message.type()}: (unsupported)"
		}

	private fun summarizeAi(message: AiMessage): String {
		val requests = message.toolExecutionRequests()
		val textPart =
			message.text()?.takeIf { it.isNotBlank() }?.let { LogPreview.of(it, MAX_CONTENT) }
		val toolsPart =
			if (requests.isEmpty()) {
				null
			} else {
				requests.joinToString(", ") { req ->
					"${req.name()}(${req.id()} args=${LogPreview.of(req.arguments(), MAX_ARGS)})"
				}
			}
		return when {
			textPart != null && toolsPart != null -> "assistant: $textPart | tools=[$toolsPart]"
			toolsPart != null -> "assistant: tools=[$toolsPart]"
			textPart != null -> "assistant: $textPart"
			else -> "assistant: (empty)"
		}
	}

	private const val MAX_CONTENT = 240
	private const val MAX_ARGS = 120
}
