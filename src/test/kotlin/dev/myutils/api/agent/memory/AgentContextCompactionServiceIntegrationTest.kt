package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.StubChatModelFactory
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

class AgentContextCompactionServiceIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var service: AgentContextCompactionService

	@Autowired
	private lateinit var adminService: AgentMemoryAdminService

	@Autowired
	private lateinit var messageRepository: AgentConversationMessageRepository

	@Autowired
	private lateinit var summaryRepository: AgentContextSummaryRepository

	@Autowired
	private lateinit var chatModelFactory: StubChatModelFactory

	@BeforeEach
	fun reset() {
		TEST_CHAT_IDS.forEach(adminService::clearDialog)
	}

	@AfterEach
	fun cleanup() {
		TEST_CHAT_IDS.forEach(adminService::clearDialog)
	}

	@Test
	fun `subsequent compactions update one rolling summary`() {
		chatModelFactory.model.resetResponses("Первый summary.", "Обновлённый summary.")
		val first = saveMessage(ROLLING_CHAT_ID, "первое")
		val second = saveMessage(ROLLING_CHAT_ID, "второе")

		val firstResult = service.compactManual(ROLLING_CHAT_ID, keepRecent = 0)
		val firstSummaryId = requireNotNull(firstResult.summaryId)
		saveMessage(ROLLING_CHAT_ID, "третье")

		val secondResult = service.compactManual(ROLLING_CHAT_ID, keepRecent = 0)
		val summaries = summaryRepository.findByChatIdOrderBySequenceAsc(ROLLING_CHAT_ID)
		val messages =
			messageRepository
				.findByChatIdAndIdGreaterThanOrderByCreatedAtAsc(ROLLING_CHAT_ID, 0)

		assertEquals(firstSummaryId, secondResult.summaryId)
		assertEquals(1, summaries.size)
		assertEquals("Обновлённый summary.", summaries.single().summaryText)
		assertEquals(first.id, summaries.single().coversMessageIdFrom)
		assertEquals(messages.last().id, summaries.single().coversMessageIdTo)
		assertEquals(3, summaries.single().sourceMessageCount)
		assertTrue(messages.all { it.isCompacted && it.compactedIntoSummaryId == firstSummaryId })

		val rollingRequest =
			chatModelFactory.model.requests
				.last()
				.messages()
				.joinToString("\n")
		assertTrue(rollingRequest.contains("Первый summary."))
		assertTrue(rollingRequest.contains("третье"))
		assertFalse(rollingRequest.contains(second.messageJson))
	}

	@Test
	fun `concurrent compactions create only one summary`() {
		chatModelFactory.model.resetResponses("Единый summary.")
		repeat(5) { index -> saveMessage(CONCURRENT_CHAT_ID, "сообщение $index") }
		val start = CountDownLatch(1)
		val executor = Executors.newFixedThreadPool(2)

		try {
			val futures =
				List(2) {
					executor.submit<AgentContextCompactionService.CompactResult> {
						start.await()
						service.compactManual(CONCURRENT_CHAT_ID, keepRecent = 0)
					}
				}
			start.countDown()
			val results = futures.map { it.get(15, TimeUnit.SECONDS) }

			assertEquals(1, results.count { it.compacted })
			assertEquals(1, summaryRepository.findByChatIdOrderBySequenceAsc(CONCURRENT_CHAT_ID).size)
			assertEquals(5, messageRepository.countByChatId(CONCURRENT_CHAT_ID))
		} finally {
			executor.shutdownNow()
		}
	}

	@Test
	fun `deleting summary makes source messages compactable again`() {
		chatModelFactory.model.resetResponses("Удаляемый summary.")
		val message = saveMessage(DELETE_CHAT_ID, "вернуть в сырой контекст")
		val summaryId = requireNotNull(service.compactManual(DELETE_CHAT_ID, keepRecent = 0).summaryId)

		adminService.deleteSummary(summaryId)

		val restored = messageRepository.findById(message.id).orElseThrow()
		assertFalse(restored.isCompacted)
		assertEquals(null, restored.compactedIntoSummaryId)
		assertEquals(0, summaryRepository.findByChatIdOrderBySequenceAsc(DELETE_CHAT_ID).size)
	}

	@Test
	fun `compaction never splits assistant tool call from its result`() {
		chatModelFactory.model.resetResponses("Summary до tool-turn.")
		val before = saveMessage(TOOL_TURN_CHAT_ID, "до инструмента")
		val assistant =
			saveRaw(
				TOOL_TURN_CHAT_ID,
				"""{"role":"assistant","tool_calls":[{"id":"a1","type":"function","function":{"name":"logWorkout","arguments":"{}"}}]}""",
			)
		val tool =
			saveRaw(
				TOOL_TURN_CHAT_ID,
				"""{"role":"tool","content":"ok","tool_call_id":"a1","name":"logWorkout"}""",
			)
		val after = saveMessage(TOOL_TURN_CHAT_ID, "после инструмента")

		val result = service.compactManual(TOOL_TURN_CHAT_ID, keepRecent = 2)

		assertEquals(1, result.messageCount)
		assertTrue(messageRepository.findById(before.id).orElseThrow().isCompacted)
		assertFalse(messageRepository.findById(assistant.id).orElseThrow().isCompacted)
		assertFalse(messageRepository.findById(tool.id).orElseThrow().isCompacted)
		assertFalse(messageRepository.findById(after.id).orElseThrow().isCompacted)
	}

	private fun saveMessage(
		chatId: Long,
		content: String,
	): AgentConversationMessage =
		messageRepository.saveAndFlush(
			AgentConversationMessage(
				chatId = chatId,
				messageJson = """{"role":"user","content":"$content"}""",
			),
		)

	private fun saveRaw(
		chatId: Long,
		messageJson: String,
	): AgentConversationMessage =
		messageRepository.saveAndFlush(
			AgentConversationMessage(
				chatId = chatId,
				messageJson = messageJson,
			),
		)

	private companion object {
		const val ROLLING_CHAT_ID = -910_001L
		const val CONCURRENT_CHAT_ID = -910_002L
		const val DELETE_CHAT_ID = -910_003L
		const val TOOL_TURN_CHAT_ID = -910_004L
		val TEST_CHAT_IDS = listOf(ROLLING_CHAT_ID, CONCURRENT_CHAT_ID, DELETE_CHAT_ID, TOOL_TURN_CHAT_ID)
	}
}
