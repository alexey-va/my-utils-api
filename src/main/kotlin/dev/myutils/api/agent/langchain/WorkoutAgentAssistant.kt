package dev.myutils.api.agent.langchain

import dev.langchain4j.service.MemoryId
import dev.langchain4j.service.SystemMessage
import dev.langchain4j.service.UserMessage

interface WorkoutAgentAssistant {
	@SystemMessage(
		"""
		{{systemPrompt}}

		{{snapshot}}
		""",
	)
	fun chat(
		@MemoryId chatId: Long,
		@UserMessage userMessage: String,
		systemPrompt: String,
		snapshot: String,
	): String
}
