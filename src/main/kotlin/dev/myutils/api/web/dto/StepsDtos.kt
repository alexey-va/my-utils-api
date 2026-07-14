package dev.myutils.api.web.dto

import com.fasterxml.jackson.databind.JsonNode
import dev.myutils.api.service.AppleHealthStepsParser

data class StepsDayDto(
	val date: String,
	val steps: Int,
)

data class StepsParsedDto(
	val source: String,
	val days: List<StepsDayDto>,
	val todaySteps: Int?,
) {
	companion object {
		fun from(parsed: AppleHealthStepsParser.Parsed): StepsParsedDto =
			StepsParsedDto(
				source = parsed.source,
				days =
					parsed.days.map { day ->
						StepsDayDto(date = day.date.toString(), steps = day.steps)
					},
				todaySteps = parsed.today?.steps,
			)
	}
}

data class HealthStepDayDto(
	val date: String,
	val steps: Int,
)

data class HealthStepsHistoryResponse(
	val days: List<HealthStepDayDto>,
	val todaySteps: Int?,
)

data class StepsIngestResponse(
	val ok: Boolean,
	val received: JsonNode?,
	val parsed: StepsParsedDto? = null,
	val savedDays: Int? = null,
)
