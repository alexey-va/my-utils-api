package dev.myutils.api.testkit

import dev.myutils.api.infra.config.Environment
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class EnvironmentTest {
	@AfterEach
	fun clearProperty() {
		System.clearProperty(Environment.PROPERTY)
	}

	@Test
	fun `TESTING uses fake clients and testing profile`() {
		assertTrue(Environment.TESTING.usesFakeClients)
		assertArrayEquals(arrayOf("test", "testing"), Environment.TESTING.testSpringProfiles())
	}

	@Test
	fun `PRODUCTION has no fake clients`() {
		assertFalse(Environment.PRODUCTION.usesFakeClients)
		assertArrayEquals(arrayOf("test"), Environment.PRODUCTION.testSpringProfiles())
	}

	@Test
	fun `resolve reads system property`() {
		System.setProperty(Environment.PROPERTY, "TESTING")
		assertEquals(Environment.TESTING, Environment.resolve(Environment.PRODUCTION))
	}
}
