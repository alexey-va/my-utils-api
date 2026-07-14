package dev.myutils.api.service

import com.fasterxml.jackson.databind.ObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import java.time.LocalDate

class AppleHealthStepsParserTest {
	private val objectMapper = ObjectMapper()
	private val today = LocalDate.parse("2026-07-14")

	@Test
	fun `parses apple shortcut multiline with empty key`() {
		val body =
			objectMapper.readTree(
				"""{"":"5780\n4464\n8065"}""",
			)

		val parsed = AppleHealthStepsParser.parse(body, today)!!

		assertEquals("apple-shortcut-multiline", parsed.source)
		assertEquals(3, parsed.days.size)
		assertEquals(LocalDate.parse("2026-07-12"), parsed.days[0].date)
		assertEquals(5780, parsed.days[0].steps)
		assertEquals(LocalDate.parse("2026-07-13"), parsed.days[1].date)
		assertEquals(4464, parsed.days[1].steps)
		assertEquals(today, parsed.days[2].date)
		assertEquals(8065, parsed.days[2].steps)
		assertEquals(8065, parsed.today?.steps)
	}

	@Test
	fun `parses real shortcut payload from prod logs`() {
		val body =
			objectMapper.readTree(
				"""{"":"5780\n4464\n18406\n9042\n5402\n3333\n6416\n12506\n10231\n7414\n4313\n6749\n7804\n6235\n14977\n6819\n8564\n10560\n6511\n7172\n11702\n10075\n4317\n4802\n5423\n7858\n16733\n17350\n4698\n4995\n8065"}""",
			)

		val parsed = AppleHealthStepsParser.parse(body, today)!!

		assertEquals(31, parsed.days.size)
		assertEquals(LocalDate.parse("2026-06-14"), parsed.days.first().date)
		assertEquals(5780, parsed.days.first().steps)
		assertEquals(today, parsed.days.last().date)
		assertEquals(8065, parsed.days.last().steps)
	}

	@Test
	fun `parses structured single day payload`() {
		val body =
			objectMapper.readTree(
				"""{"date":"2026-07-14","steps":8432,"source":"apple-shortcut"}""",
			)

		val parsed = AppleHealthStepsParser.parse(body, today)!!

		assertEquals("structured", parsed.source)
		assertEquals(listOf(AppleHealthStepsParser.Day(today, 8432)), parsed.days)
		assertEquals(8432, parsed.today?.steps)
	}

	@Test
	fun `returns null for unknown payload`() {
		assertNull(AppleHealthStepsParser.parse(objectMapper.readTree("""{"foo":"bar"}"""), today))
	}

	@Test
	fun `fails on non numeric line`() {
		val body = objectMapper.readTree("""{"":"100\noops"}""")

		assertThrows<IllegalArgumentException> {
			AppleHealthStepsParser.parse(body, today)
		}
	}
}
