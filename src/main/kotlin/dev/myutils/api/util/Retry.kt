package dev.myutils.api.util

import kotlinx.coroutines.delay
import org.slf4j.Logger
import org.springframework.web.client.ResourceAccessException
import org.springframework.web.client.RestClientResponseException
import java.io.IOException

fun isTransientFailure(ex: Throwable): Boolean =
	when (ex) {
		is ResourceAccessException,
		is IOException,
		-> true
		is RestClientResponseException ->
			ex.statusCode.is5xxServerError || ex.statusCode.value() == 429
		else -> ex.cause?.let(::isTransientFailure) == true
	}

fun <T> retryBlocking(
	name: String,
	policy: RetryPolicy,
	log: Logger,
	isRetryable: (Throwable) -> Boolean = ::isTransientFailure,
	block: () -> T,
): T {
	var attempt = 1
	while (true) {
		try {
			return block()
		} catch (ex: Throwable) {
			if (attempt >= policy.maxAttempts || !isRetryable(ex)) {
				throw ex
			}
			val waitMs = policy.delayBeforeRetry(attempt)
			log.warn(
				"{} attempt {}/{} failed: {} — retry in {}ms",
				name,
				attempt,
				policy.maxAttempts,
				describeThrowable(ex),
				waitMs,
			)
			Thread.sleep(waitMs)
			attempt++
		}
	}
}

suspend fun <T> retry(
	name: String,
	policy: RetryPolicy,
	log: Logger,
	isRetryable: (Throwable) -> Boolean = ::isTransientFailure,
	block: suspend () -> T,
): T {
	var attempt = 1
	while (true) {
		try {
			return block()
		} catch (ex: Throwable) {
			if (attempt >= policy.maxAttempts || !isRetryable(ex)) {
				throw ex
			}
			val waitMs = policy.delayBeforeRetry(attempt)
			log.warn(
				"{} attempt {}/{} failed: {} — retry in {}ms",
				name,
				attempt,
				policy.maxAttempts,
				describeThrowable(ex),
				waitMs,
			)
			delay(waitMs)
			attempt++
		}
	}
}
