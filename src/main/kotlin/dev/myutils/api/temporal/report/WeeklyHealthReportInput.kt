package dev.myutils.api.temporal.report

import com.fasterxml.jackson.annotation.JsonCreator
import com.fasterxml.jackson.annotation.JsonProperty

data class WeeklyHealthReportInput
	@JsonCreator(mode = JsonCreator.Mode.PROPERTIES)
	constructor(
		@JsonProperty("chatId") val chatId: Long,
		@JsonProperty("zoneId") val zoneId: String = "Europe/Moscow",
		@JsonProperty("lookbackDays") val lookbackDays: Int = 90,
	)

data class WeeklyHealthReportActivityInput
	@JsonCreator(mode = JsonCreator.Mode.PROPERTIES)
	constructor(
		@JsonProperty("chatId") val chatId: Long,
		@JsonProperty("reportDate") val reportDate: String,
		@JsonProperty("lookbackDays") val lookbackDays: Int,
	)
