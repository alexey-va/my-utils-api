package dev.myutils.api.telegram

import org.slf4j.Logger
import java.io.IOException

internal object TelegramApiSupport {
	fun describeError(ex: Throwable): String {
		val parts = mutableListOf<String>()
		parts.add(ex.javaClass.simpleName)
		ex.message?.takeIf { it.isNotBlank() }?.let { parts.add(it) }
		(ex.cause ?: ex).let { root ->
			if (root !== ex && root.message?.isNotBlank() == true) {
				parts.add("cause=${root.javaClass.simpleName}: ${root.message}")
			}
		}
		return parts.joinToString(" | ")
	}

	inline fun <T> Logger.timed(
		operation: String,
		block: () -> T,
	): T {
		val startedAt = System.nanoTime()
		info("Telegram {} started", operation)
		return try {
			block().also {
				info("Telegram {} finished in {}ms", operation, elapsedMs(startedAt))
			}
		} catch (ex: IOException) {
			warn(
				"Telegram {} failed after {}ms: {}",
				operation,
				elapsedMs(startedAt),
				describeError(ex),
				ex,
			)
			throw ex
		} catch (ex: Exception) {
			warn(
				"Telegram {} failed after {}ms: {}",
				operation,
				elapsedMs(startedAt),
				describeError(ex),
				ex,
			)
			throw ex
		}
	}

	private fun elapsedMs(startedAt: Long): Long = (System.nanoTime() - startedAt) / 1_000_000
}
