package dev.myutils.api.agent

/** Keeps explicit workout notation grounded in the current user message. */
object AgentToolArgumentsGrounding {
	private val workoutNotation =
		Regex(
			"""(?<![\d.,/])\d+(?:[.,]\d+)?\s+(?:\d+\s*[*xх×]\s*)?\d+(?:\s*/\s*\d+)+(?![\d/])""",
			RegexOption.IGNORE_CASE,
		)

	fun ground(
		toolName: String,
		args: Map<String, String?>,
		userMessage: String?,
	): Map<String, String?> {
		if (AgentToolCatalog.normalizeName(toolName) != "log_workout") {
			return args
		}
		val literal = userMessage?.let { workoutNotation.find(it)?.value }?.trim() ?: return args
		return args + ("notation" to literal)
	}
}
