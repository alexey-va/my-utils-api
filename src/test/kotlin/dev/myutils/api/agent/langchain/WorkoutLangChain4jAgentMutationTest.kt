package dev.myutils.api.agent.langchain

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutAgentContextBuilder
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.agent.memory.AgentConversationStore
import dev.myutils.api.agent.memory.AgentMemoryAssembler
import dev.myutils.api.agent.memory.AgentUserFactsService
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.temporal.agent.ToolCallDto
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.verifyNoInteractions

class WorkoutLangChain4jAgentMutationTest {
	private val toolsService = mock<WorkoutToolsService>()
	private val agent =
		WorkoutLangChain4jAgent(
			properties = MyUtilsProperties(),
			chatModelFactory = mock(),
			conversationStore = mock<AgentConversationStore>(),
			memoryAssembler = mock<AgentMemoryAssembler>(),
			userFacts = mock<AgentUserFactsService>(),
			contextBuilder = mock<WorkoutAgentContextBuilder>(),
			toolsService = toolsService,
			objectMapper = ObjectMapper(),
		)

	@Test
	fun `memory path blocks delete from a read only image turn`() {
		val result =
			agent.executeToolCall(
				chatId = 303179278L,
				call =
					ToolCallDto(
						id = "tc-delete",
						name = "deleteWorkout",
						argumentsJson = """{"exercise_name":"Плечи","performed_on":"2026-07-31"}""",
					),
				mutationAuthorizationText = "Что осталось на этой неделе?",
			)

		assertTrue(ToolExecutionFeedback.isFailure(result))
		verifyNoInteractions(toolsService)
	}
}
