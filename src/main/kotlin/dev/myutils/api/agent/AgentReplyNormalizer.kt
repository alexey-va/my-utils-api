package dev.myutils.api.agent

/** Deterministic last-mile cleanup for model text sent with Telegram HTML parse mode. */
object AgentReplyNormalizer {
	private val fencedCodeMarker = Regex("""(?m)^[ \t]*```(?:\w+)?[ \t]*$""")
	private val markdownHeading = Regex("""(?m)^[ \t]{0,3}#{1,6}[ \t]+(.+?)[ \t]*$""")
	private val markdownBold = Regex("""\*\*([^*\n]+)\*\*|__([^_\n]+)__""")
	private val markdownCode = Regex("""`([^`\n]+)`""")
	private val markdownBullet = Regex("""(?m)^[ \t]*[-*+][ \t]+""")
	private val markdownLink = Regex("""\[([^\]\n]+)]\((https?://[^)\s]+)\)""")
	private val horizontalWhitespace = Regex("""[ \t]{2,}""")
	private val lineLeadingWhitespace = Regex("""(?m)^[ \t]+""")
	private val whitespaceBeforePunctuation = Regex("""[ \t]+([,.!?;:])""")
	private val excessiveBlankLines = Regex("""\n{3,}""")

	fun forTelegram(raw: String): String {
		var text = raw.trim()
		text = fencedCodeMarker.replace(text, "")
		text = markdownHeading.replace(text) { match -> "<b>${match.groupValues[1]}</b>" }
		text = markdownBold.replace(text) { match ->
			val content = match.groupValues[1].ifEmpty { match.groupValues[2] }
			"<b>$content</b>"
		}
		text = markdownCode.replace(text) { match -> "<code>${match.groupValues[1]}</code>" }
		text = markdownBullet.replace(text, "• ")
		text = markdownLink.replace(text) { match -> "${match.groupValues[1]} (${match.groupValues[2]})" }
		text = text.replace("`", "")
		text = horizontalWhitespace.replace(text, " ")
		text = lineLeadingWhitespace.replace(text, "")
		text = whitespaceBeforePunctuation.replace(text) { match -> match.groupValues[1] }
		text = excessiveBlankLines.replace(text, "\n\n")
		return text.trim().ifEmpty { "Готово." }
	}
}
