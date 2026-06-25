package dev.myutils.api.agent.memory

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class FactConfidenceTest {
	@Test
	fun `normalizes out of range`() {
		assertEquals(1.0, FactConfidence.normalize(1.5))
		assertEquals(0.0, FactConfidence.normalize(-0.2))
	}

	@Test
	fun `uses agent default when null`() {
		assertEquals(FactConfidence.AGENT_DEFAULT, FactConfidence.normalize(null))
	}
}
