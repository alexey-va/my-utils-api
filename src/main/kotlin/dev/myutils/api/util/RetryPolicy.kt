package dev.myutils.api.util

data class RetryPolicy(
	val maxAttempts: Int = 3,
	val initialDelayMs: Long = 1_000,
	val maxDelayMs: Long = 30_000,
	val multiplier: Double = 2.0,
) {
	init {
		require(maxAttempts >= 1) { "maxAttempts must be >= 1" }
		require(initialDelayMs >= 0) { "initialDelayMs must be >= 0" }
	}

	fun delayBeforeRetry(attempt: Int): Long {
		var delay = initialDelayMs
		repeat((attempt - 1).coerceAtLeast(0)) {
			delay = (delay * multiplier).toLong().coerceAtMost(maxDelayMs)
		}
		return delay
	}
}
