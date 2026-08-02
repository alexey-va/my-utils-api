package dev.myutils.api.properties

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.web.server.ResponseStatusException

class PropertyTest {
	private val mapper = jacksonObjectMapper()

	@Test
	fun `AppProperties ALL discovers runtime properties without recursion`() {
		assertTrue(AppProperties.ALL.size >= 12)
		assertTrue(AppProperties.ALL.any { it.key == "agent.context.recent-entries" })
		assertTrue(AppProperties.ALL.any { it.key == "openrouter.model" })
		assertTrue(AppProperties.ALL.any { it.key == "agent.system-prompt" })
		assertEquals(PropertyEditor.TEXTAREA, AppProperties.AGENT_SYSTEM_PROMPT.editor)
		assertEquals(listOf("agent", "telegram"), AppProperties.AGENT_SYSTEM_PROMPT.tags)
		assertTrue(AppProperties.TEMPORAL_EVENING_REMINDER_ENABLED.tags.contains("temporal"))
	}

	@Test
	fun `BooleanProperty parses stored value`() {
		val p = AppProperties.TEMPORAL_EVENING_REMINDER_ENABLED
		assertEquals(true, p.deserialize("true", mapper))
		assertEquals(false, p.deserialize("false", mapper))
		assertEquals(false, p.default)
	}

	@Test
	fun `IntProperty has typed default and range`() {
		val p = AppProperties.TEMPORAL_EVENING_REMINDER_MINUTE
		assertEquals(0, p.default)
		assertEquals("0", p.serialize(0, mapper))
		val hour = AppProperties.TEMPORAL_EVENING_REMINDER_HOUR
		assertThrows(ResponseStatusException::class.java) {
			hour.normalize(ObjectMapper().readTree("25"), mapper)
		}
	}

	@Test
	fun `StringProperty accepts openrouter model`() {
		val p = AppProperties.OPENROUTER_MODEL
		assertEquals("openai/gpt-5.4-mini", p.default)
		assertEquals(
			"openai/gpt-5.4-mini",
			p.deserialize(p.serialize(p.default, mapper), mapper),
		)
	}

	@Test
	fun `StringProperty rejects invalid zone`() {
		val p = AppProperties.TEMPORAL_ZONE_ID
		assertThrows(ResponseStatusException::class.java) {
			p.deserialize("\"Not/A/Zone\"", mapper)
		}
	}

	@Test
	fun `DataProperty parses typed data class`() {
		val p =
			dataProperty(
				key = "test.schedule",
				description = "test",
				default = EveningReminderSchedule(enabled = true, hour = 20, minute = 0),
			)
		assertEquals(PropertyType.OBJECT, p.type)
		assertEquals("EveningReminderSchedule", p.objectType)
		val parsed =
			p.deserialize(
				"""{"enabled":true,"hour":21,"minute":30}""",
				mapper,
			)
		assertEquals(EveningReminderSchedule(enabled = true, hour = 21, minute = 30), parsed)
	}

	@Test
	fun `DataProperty rejects invalid object`() {
		val p =
			dataProperty(
				key = "test.schedule",
				description = "test",
				default = EveningReminderSchedule(enabled = false, hour = 20, minute = 0),
				validate = { it.hour in 0..23 },
			)
		assertThrows(ResponseStatusException::class.java) {
			p.deserialize("""{"enabled":true,"hour":99,"minute":0}""", mapper)
		}
	}
}

data class EveningReminderSchedule(
	val enabled: Boolean,
	val hour: Int,
	val minute: Int,
)
