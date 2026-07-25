package dev.myutils.api.temporal.reminder

import com.fasterxml.jackson.annotation.JsonCreator
import com.fasterxml.jackson.annotation.JsonProperty

/** Аргументы долгоживущего workflow ежедневного напоминания. */
data class ReminderWorkflowInput
	@JsonCreator(mode = JsonCreator.Mode.PROPERTIES)
	constructor(
		@JsonProperty("chatId") val chatId: Long,
		@JsonProperty("zoneId") val zoneId: String = "Europe/Moscow",
		@JsonProperty("hour") val hour: Int = 20,
		@JsonProperty("minute") val minute: Int = 0,
	)
