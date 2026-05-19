package dev.myutils.api.temporal

import dev.myutils.api.temporal.notification.NotificationWorkflowInput
import dev.myutils.api.temporal.reminder.ReminderWorkflowInput
import io.temporal.common.converter.DataConverter
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class TemporalDataConverterConfigurationTest {
	private val converter: DataConverter = TemporalDataConverterConfiguration().temporalDataConverter()

	@Test
	fun `round-trips ReminderWorkflowInput`() {
		val input = ReminderWorkflowInput(chatId = 303179278L, hour = 20, minute = 30)
		val payload = converter.toPayload(input).orElseThrow()
		val restored = converter.fromPayload(payload, ReminderWorkflowInput::class.java, ReminderWorkflowInput::class.java)
		assertEquals(input, restored)
	}

	@Test
	fun `round-trips NotificationWorkflowInput`() {
		val input =
			NotificationWorkflowInput(
				chatId = 1L,
				message = "test",
				deliverAtEpochMillis = 1_700_000_000_000L,
			)
		val payload = converter.toPayload(input).orElseThrow()
		val restored =
			converter.fromPayload(
				payload,
				NotificationWorkflowInput::class.java,
				NotificationWorkflowInput::class.java,
			)
		assertEquals(input, restored)
	}
}
