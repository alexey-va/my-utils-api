package dev.myutils.api.agent.memory

import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage

/** Убирает оборванные tool-turn'ы (assistant с tool_calls без всех tool results). */
internal object AgentMemorySanitizer {
	fun dropIncompleteToolTurns(messages: List<LcChatMessage>): List<LcChatMessage> {
		val result = mutableListOf<LcChatMessage>()
		var index = 0
		while (index < messages.size) {
			val message = messages[index]
			if (message is AiMessage && message.toolExecutionRequests().isNotEmpty()) {
				val requiredIds = message.toolExecutionRequests().map { it.id() }.toSet()
				var cursor = index + 1
				val toolResults = mutableListOf<ToolExecutionResultMessage>()
				while (cursor < messages.size && messages[cursor] is ToolExecutionResultMessage) {
					toolResults.add(messages[cursor] as ToolExecutionResultMessage)
					cursor++
				}
				val resultIds = toolResults.map { it.id() }.toSet()
				if (resultIds.containsAll(requiredIds)) {
					result.add(message)
					result.addAll(toolResults)
				}
				index = cursor
			} else {
				result.add(message)
				index++
			}
		}
		return result
	}
}
