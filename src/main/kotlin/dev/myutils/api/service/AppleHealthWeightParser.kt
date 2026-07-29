package dev.myutils.api.service

import com.fasterxml.jackson.databind.JsonNode
import java.math.BigDecimal
import java.time.LocalDate

/**
 * Apple Shortcuts sends one grouped weight value per day in the empty JSON key.
 * The last line represents today; blank or zero lines preserve missing calendar days.
 */
object AppleHealthWeightParser {
	data class Day(
		val date: LocalDate,
		val weightKg: BigDecimal,
	)

	data class Parsed(
		val receivedDays: Int,
		val days: List<Day>,
	)

	fun parse(
		body: JsonNode?,
		today: LocalDate,
	): Parsed? {
		if (body == null || !body.isObject) return null
		val multiline =
			body.get("")
				?.takeIf { it.isTextual }
				?.asText()
				?: return null

		val normalized = multiline.replace("\r\n", "\n").trimEnd('\r', '\n')
		if (normalized.isBlank()) return null
		val lines = normalized.split('\n')
		val days =
			lines.mapIndexedNotNull { index, line ->
				val weightKg = parseWeight(line) ?: return@mapIndexedNotNull null
				val daysAgo = lines.lastIndex - index
				Day(
					date = today.minusDays(daysAgo.toLong()),
					weightKg = weightKg,
				)
			}

		return Parsed(
			receivedDays = lines.size,
			days = days,
		)
	}

	private fun parseWeight(line: String): BigDecimal? {
		val match = DECIMAL.find(line.trim()) ?: return null
		val value = match.value.replace(',', '.').toBigDecimal()
		return value.takeIf { it.signum() > 0 }
	}

	private val DECIMAL = Regex("""\d+(?:[.,]\d+)?""")
}
