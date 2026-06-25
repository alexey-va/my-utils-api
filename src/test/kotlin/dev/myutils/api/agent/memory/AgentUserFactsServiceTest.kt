package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentUserFact
import dev.myutils.api.domain.AgentUserFactRepository
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever
import java.util.Optional
import java.util.UUID

class AgentUserFactsServiceTest {
	private val repository: AgentUserFactRepository = mock()
	private val service = AgentUserFactsService(repository)

	@Test
	fun `remember saves fact`() {
		val id = UUID.randomUUID()
		whenever(repository.save(any())).thenAnswer {
			val fact = it.arguments[0] as AgentUserFact
			AgentUserFact(id = id, chatId = fact.chatId, content = fact.content)
		}

		val result = service.remember(chatId = 42L, content = "  не любит присед  ")

		assertTrue(result.contains(id.toString()))
		assertTrue(result.contains("не любит присед"))
		verify(repository).save(any())
	}

	@Test
	fun `formatForPrompt lists facts with ids`() {
		val id = UUID.randomUUID()
		whenever(repository.findByChatIdOrderByUpdatedAtDesc(7L)).thenReturn(
			listOf(AgentUserFact(id = id, chatId = 7L, content = "травма колена")),
		)

		val formatted = service.formatForPrompt(7L)

		assertTrue(formatted.contains(id.toString()))
		assertTrue(formatted.contains("травма колена"))
	}

	@Test
	fun `forget removes fact for chat`() {
		val id = UUID.randomUUID()
		val fact = AgentUserFact(id = id, chatId = 9L, content = "old")
		whenever(repository.findByIdAndChatId(id, 9L)).thenReturn(Optional.of(fact))

		val result = service.forget(9L, id.toString())

		assertEquals("Удалил факт [$id].", result)
		verify(repository).delete(fact)
	}
}
