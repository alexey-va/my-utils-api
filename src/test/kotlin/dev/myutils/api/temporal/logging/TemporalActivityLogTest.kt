package dev.myutils.api.temporal.logging

import org.junit.jupiter.api.Assertions.assertDoesNotThrow
import org.junit.jupiter.api.Test
import org.slf4j.LoggerFactory

class TemporalActivityLogTest {
	private val log = LoggerFactory.getLogger(javaClass)

	@Test
	fun `enrich is no-op outside activity context`() {
		assertDoesNotThrow {
			TemporalActivityLog
				.enrich(log.atInfo().setMessage("test").addKeyValue("chatId", 1L))
				.log()
		}
	}
}
