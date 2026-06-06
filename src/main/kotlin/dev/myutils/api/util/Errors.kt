package dev.myutils.api.util

fun describeThrowable(ex: Throwable): String {
	val parts = mutableListOf(ex.javaClass.simpleName)
	ex.message?.takeIf { it.isNotBlank() }?.let { parts.add(it) }
	val cause = ex.cause
	if (cause != null && cause !== ex && !cause.message.isNullOrBlank()) {
		parts.add("cause=${cause.javaClass.simpleName}: ${cause.message}")
	}
	return parts.joinToString(" | ")
}
