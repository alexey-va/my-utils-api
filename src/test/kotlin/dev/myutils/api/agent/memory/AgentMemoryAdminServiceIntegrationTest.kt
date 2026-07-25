package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentContextSummary
import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired

class AgentMemoryAdminServiceIntegrationTest : IntegrationTestBase() {
	@Autowired
	private lateinit var service: AgentMemoryAdminService

	@Autowired
	private lateinit var messageRepository: AgentConversationMessageRepository

	@Autowired
	private lateinit var summaryRepository: AgentContextSummaryRepository

	@Test
	fun `clear dialog deletes compacted messages before their summary`() {
		val chatId = -900_001L
		service.clearDialog(chatId)

		val message =
			messageRepository.saveAndFlush(
				AgentConversationMessage(
					chatId = chatId,
					messageJson = """{"role":"user","content":"test"}""",
				),
			)
		val summary =
			summaryRepository.saveAndFlush(
				AgentContextSummary(
					chatId = chatId,
					sequence = 1,
					summaryText = "test summary",
					coversMessageIdFrom = message.id,
					coversMessageIdTo = message.id,
					sourceMessageCount = 1,
				),
			)
		message.compactedIntoSummaryId = summary.id
		message.isCompacted = true
		messageRepository.saveAndFlush(message)

		service.clearDialog(chatId)

		assertEquals(0, messageRepository.countByChatId(chatId))
		assertEquals(0, summaryRepository.findByChatIdOrderBySequenceAsc(chatId).size)
	}
}
