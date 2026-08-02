package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentReplyNormalizerTest {
	@Test
	fun `converts common markdown to telegram html`() {
		val normalized =
			AgentReplyNormalizer.forTelegram(
				"""
				### План
				**Воскресенье, 02.08** — отдых. Потом `70 кг 3×10`.
				""".trimIndent(),
			)

		assertEquals(
			"""
			<b>План</b>
			<b>Воскресенье, 02.08</b> — отдых. Потом <code>70 кг 3×10</code>.
			""".trimIndent(),
			normalized,
		)
		assertFalse(normalized.contains("**"))
		assertFalse(normalized.contains("###"))
	}

	@Test
	fun `removes mirrored profanity without damaging the answer`() {
		val normalized = AgentReplyNormalizer.forTelegram("Бля, завтра понедельник, 03.08 — отдых.")

		assertFalse(normalized.lowercase().contains("бля"))
		assertTrue(normalized.contains("понедельник, 03.08"))
	}

	@Test
	fun `preserves existing telegram html`() {
		assertEquals("<b>Сегодня</b> — отдых.", AgentReplyNormalizer.forTelegram("<b>Сегодня</b> — отдых."))
	}
}
