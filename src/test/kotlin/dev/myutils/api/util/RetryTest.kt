package dev.myutils.api.util

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test
import org.slf4j.LoggerFactory
import java.io.IOException
import java.util.concurrent.atomic.AtomicInteger

class RetryTest {
	private val log = LoggerFactory.getLogger(RetryTest::class.java)

	@Test
	fun `retry succeeds after transient failures`() {
		val attempts = AtomicInteger()
		val result =
			retryBlocking(
				name = "test",
				policy = RetryPolicy(maxAttempts = 3, initialDelayMs = 0),
				log = log,
			) {
				if (attempts.incrementAndGet() < 3) {
					throw IOException("timeout")
				}
				"ok"
			}
		assertEquals("ok", result)
		assertEquals(3, attempts.get())
	}

	@Test
	fun `retry stops on non-retryable error`() {
		val attempts = AtomicInteger()
		assertThrows(IllegalStateException::class.java) {
			retryBlocking(
				name = "test",
				policy = RetryPolicy(maxAttempts = 5, initialDelayMs = 0),
				log = log,
			) {
				attempts.incrementAndGet()
				throw IllegalStateException("bad request")
			}
		}
		assertEquals(1, attempts.get())
	}

	@Test
	fun `delay grows with attempt`() {
		val policy = RetryPolicy(initialDelayMs = 100, multiplier = 2.0, maxDelayMs = 1_000)
		assertEquals(100, policy.delayBeforeRetry(1))
		assertEquals(200, policy.delayBeforeRetry(2))
		assertEquals(400, policy.delayBeforeRetry(3))
	}
}
