package dev.myutils.api.agent

import kotlin.math.roundToInt

/** Keeps explicit workout notation grounded in the current user message. */
object AgentToolArgumentsGrounding {
	private val workoutNotation =
		Regex(
			"""(?<![\d.,/])\d+(?:[.,]\d+)?\s+(?:\d+\s*[*xх×]\s*)?\d+(?:\s*/\s*\d+)+(?![\d/])""",
			RegexOption.IGNORE_CASE,
		)
	private val poundWeight =
		Regex(
			"""(?<![\d.,])(\d+(?:[.,]\d+)?)\s*(?:lb|lbs|фунт(?:а|ов)?)(?![\p{L}])""",
			RegexOption.IGNORE_CASE,
		)
	private val leadingWeight = Regex("""^\s*\d+(?:[.,]\d+)?""")

	fun ground(
		toolName: String,
		args: Map<String, String?>,
		userMessage: String?,
	): Map<String, String?> {
		if (AgentToolCatalog.normalizeName(toolName) != "log_workout") {
			return args
		}
		val pounds =
			userMessage
				?.let(poundWeight::find)
				?.groupValues
				?.get(1)
				?.replace(',', '.')
				?.toDoubleOrNull()
		if (pounds != null) {
			val notation = args["notation"]?.trim().orEmpty()
			if (notation.isNotEmpty() && leadingWeight.containsMatchIn(notation)) {
				val kilograms = (pounds * POUNDS_TO_KILOGRAMS).roundToInt().coerceAtLeast(1)
				return args + ("notation" to leadingWeight.replaceFirst(notation, kilograms.toString()))
			}
		}
		val literal = userMessage?.let { workoutNotation.find(it)?.value }?.trim() ?: return args
		return args + ("notation" to literal)
	}

	private const val POUNDS_TO_KILOGRAMS = 0.45359237
}
