package dev.myutils.api.service

import java.time.DayOfWeek
import java.time.LocalDate
import java.time.temporal.TemporalAdjusters

object WorkoutMuscleGroups {
	val LABEL_RU: Map<String, String> =
		mapOf(
			"chest" to "грудь",
			"back" to "спина",
			"legs" to "ноги",
			"shoulders" to "плечи",
			"arms" to "руки",
			"core" to "кор",
			"other" to "другое",
		)

	fun labelRu(code: String): String = LABEL_RU[code.lowercase()] ?: code

	/** Понедельник текущей недели (календарная неделя, Europe/Moscow). */
	fun weekStartMonday(today: LocalDate): LocalDate =
		today.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))
}
