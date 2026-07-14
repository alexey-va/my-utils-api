package dev.myutils.api.service

import com.fasterxml.jackson.databind.JsonNode
import java.time.LocalDate

/**
 * Apple Shortcut шлёт шаги одной многострочной строкой в пустом JSON-ключе:
 * `{"":"5780\n4464\n...\n8065"}` — сверху старые дни, последняя строка = сегодня.
 */
object AppleHealthStepsParser {
	data class Day(
		val date: LocalDate,
		val steps: Int,
	)

	data class Parsed(
		val source: String,
		val days: List<Day>,
	) {
		val today: Day? = days.lastOrNull()
	}

	fun parse(
		body: JsonNode?,
		today: LocalDate,
	): Parsed? {
		if (body == null || body.isNull) return null

		parseShortcutMultiline(body, today)?.let { return it }
		parseStructured(body, today)?.let { return it }

		return null
	}

	private fun parseShortcutMultiline(
		body: JsonNode,
		today: LocalDate,
	): Parsed? {
		val multiline = extractShortcutMultiline(body) ?: return null
		val counts = parseStepCounts(multiline)
		if (counts.isEmpty()) return null

		val days =
			counts.mapIndexed { index, steps ->
				val daysAgo = counts.lastIndex - index
				Day(date = today.minusDays(daysAgo.toLong()), steps = steps)
			}

		return Parsed(source = "apple-shortcut-multiline", days = days)
	}

	private fun parseStructured(
		body: JsonNode,
		today: LocalDate,
	): Parsed? {
		if (!body.isObject) return null

		val stepsNode = body.get("steps") ?: return null
		if (!stepsNode.canConvertToInt()) return null

		val steps = stepsNode.asInt()
		val date =
			body.get("date")
				?.takeIf { it.isTextual }
				?.asText()
				?.let(LocalDate::parse)
				?: today

		return Parsed(
			source = "structured",
			days = listOf(Day(date = date, steps = steps)),
		)
	}

	private fun extractShortcutMultiline(body: JsonNode): String? {
		if (!body.isObject) return null

		body.get("")
			?.takeIf { it.isTextual }
			?.asText()
			?.let { return it }

		val fields = body.fields()
		while (fields.hasNext()) {
			val node = fields.next().value
			if (node.isTextual && node.asText().contains('\n')) {
				return node.asText()
			}
		}

		return null
	}

	private fun parseStepCounts(multiline: String): List<Int> =
		multiline
			.lines()
			.map { it.trim() }
			.filter { it.isNotEmpty() }
			.map { line ->
				line.toIntOrNull()
					?: throw IllegalArgumentException("Не число в строке шагов: «$line»")
			}
}
