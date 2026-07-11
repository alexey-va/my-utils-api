package dev.myutils.api.agent

/** Метаданные инструментов агента (в т.ч. для Temporal workflow). */
object AgentToolCatalog {
	private val immediateReturnTools =
		setOf(
			"send_rich_message",
			"sendRichMessage",
		)

	fun isImmediateReturn(toolName: String): Boolean = normalizeName(toolName) in immediateReturnTools

	fun normalizeName(toolName: String): String = camelToSnake(toolName)

	private fun camelToSnake(value: String): String =
		value
			.replace(Regex("([a-z0-9])([A-Z])")) { "${it.groupValues[1]}_${it.groupValues[2]}" }
			.lowercase()
}
