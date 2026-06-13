package dev.myutils.api.agent

import com.fasterxml.jackson.databind.ObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ToolArgumentsJsonParserTest {
	private val objectMapper = ObjectMapper()

	@Test
	fun `parses valid json`() {
		val parsed =
			ToolArgumentsJsonParser.parse(
				objectMapper,
				"""{"days":"2026-06-09","from":"2026-06-01","to":"2026-06-02"}""",
			)
		assertTrue(parsed is ToolArgumentsJsonParser.ParseResult.Ok)
		val ok = parsed as ToolArgumentsJsonParser.ParseResult.Ok
		assertEquals("2026-06-09", ok.args["days"])
		assertEquals("2026-06-01", ok.args["from"])
	}

	@Test
	fun `repairs unquoted dates in json syntax only`() {
		val parsed =
			ToolArgumentsJsonParser.parse(
				objectMapper,
				"""{"days": "2026-06-09", "from": 20226-06-09, "to": 20226-06-09}""",
			)
		assertTrue(parsed is ToolArgumentsJsonParser.ParseResult.Ok)
		val ok = parsed as ToolArgumentsJsonParser.ParseResult.Ok
		assertEquals("2026-06-09", ok.args["days"])
		assertEquals("20226-06-09", ok.args["from"])
		assertEquals("20226-06-09", ok.args["to"])
	}

	@Test
	fun `returns error for completely invalid json`() {
		val parsed = ToolArgumentsJsonParser.parse(objectMapper, """{"days":""")
		assertTrue(parsed is ToolArgumentsJsonParser.ParseResult.Error)
	}
}
