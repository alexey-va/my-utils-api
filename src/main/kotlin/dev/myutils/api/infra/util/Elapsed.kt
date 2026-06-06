package dev.myutils.api.infra.util

inline fun <T> measureMillis(block: () -> T): Pair<T, Long> {
	val started = System.nanoTime()
	val result = block()
	val ms = (System.nanoTime() - started) / 1_000_000
	return result to ms
}
