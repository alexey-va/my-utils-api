package dev.myutils.api.agent.langchain

/** Отсекает типичные «отказы»/не-русские ответы fast-моделей. */
internal object AgentReplyGuard {
	private val chineseRefusalMarkers =
		listOf(
			"作为一个人工智能语言模型",
			"我还没学习如何回答这个问题",
		)

	fun looksInvalidForRussianUser(text: String): Boolean {
		val trimmed = text.trim()
		if (trimmed.isEmpty()) {
			return false
		}
		if (chineseRefusalMarkers.any { trimmed.contains(it) }) {
			return true
		}
		val han = trimmed.count { Character.UnicodeScript.of(it.code) == Character.UnicodeScript.HAN }
		val cyrillic = trimmed.count { Character.UnicodeScript.of(it.code) == Character.UnicodeScript.CYRILLIC }
		return han >= 4 && han > cyrillic
	}
}
