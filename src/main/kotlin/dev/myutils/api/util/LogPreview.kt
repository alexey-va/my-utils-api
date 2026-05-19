package dev.myutils.api.util

object LogPreview {
	fun of(
		text: String?,
		max: Int = 160,
	): String {
		if (text.isNullOrBlank()) {
			return "(empty)"
		}
		val oneLine = text.replace('\n', ' ').trim()
		return if (oneLine.length <= max) oneLine else oneLine.take(max) + "…"
	}
}
