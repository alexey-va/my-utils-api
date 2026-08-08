package dev.myutils.api.agent.memory

import dev.myutils.api.infra.openrouter.ChatMessage
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneId

class AgentMemoryAssemblerTest {
	@Test
	fun `timestamps relative dates in raw dialog history`() {
		val result =
			AgentMemoryContextLabels.timestampMessage(
				message = ChatMessage(role = "user", content = "Вчера присед 60 кг"),
				createdAt = Instant.parse("2026-08-03T19:44:40Z"),
				zoneId = ZoneId.of("Europe/Moscow"),
			)

		assertEquals(
			"[Отправлено 03.08.2026 22:44 Europe/Moscow] Вчера присед 60 кг",
			result.content,
		)
	}

	@Test
	fun `marks compacted summary as historical instead of current week state`() {
		val summary =
			AgentMemoryContextLabels.historicalSummary(
				sequence = 1,
				summaryText = "На неделе закрыли ноги.",
			)

		assertTrue(summary.text().contains("не источник текущей даты, текущей недели"))
		assertTrue(summary.text().contains("используй только свежий снимок"))
	}

	@Test
	fun `keeps tool messages untouched to preserve tool turn adjacency`() {
		val message = ChatMessage(role = "tool", content = "Записано", toolCallId = "tc-1", name = "log_workout")

		val result =
			AgentMemoryContextLabels.timestampMessage(
				message,
				Instant.parse("2026-08-03T19:44:40Z"),
				ZoneId.of("Europe/Moscow"),
			)

		assertEquals(message, result)
	}
}
