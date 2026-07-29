package dev.myutils.api.service

import com.fasterxml.jackson.databind.ObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.math.BigDecimal
import java.time.LocalDate

class AppleHealthWeightParserTest {
	private val objectMapper = ObjectMapper()
	private val today = LocalDate.parse("2026-07-29")

	@Test
	fun `maps multiline weights to dates while preserving missing days`() {
		val body =
			objectMapper.readTree(
				"""{"":"83.1\n\n82,7 kg\n0\n81.4 kg"}""",
			)

		val parsed = AppleHealthWeightParser.parse(body, today)!!

		assertEquals(5, parsed.receivedDays)
		assertEquals(
			listOf(
				AppleHealthWeightParser.Day(LocalDate.parse("2026-07-25"), BigDecimal("83.1")),
				AppleHealthWeightParser.Day(LocalDate.parse("2026-07-27"), BigDecimal("82.7")),
				AppleHealthWeightParser.Day(today, BigDecimal("81.4")),
			),
			parsed.days,
		)
	}
}
