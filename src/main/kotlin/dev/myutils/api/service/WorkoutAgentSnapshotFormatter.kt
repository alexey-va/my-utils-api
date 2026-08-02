package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.WorkoutEntry
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Текстовый снимок дневника для промпта агента (неделя, группы мышц, прогрессия). */
object WorkoutAgentSnapshotFormatter {
	private val dateFmt = DateTimeFormatter.ofPattern("dd.MM")
	private val weekdayFmt = DateTimeFormatter.ofPattern("EEEE", Locale.forLanguageTag("ru"))

	fun format(
		today: LocalDate,
		nowLine: String,
		exercises: List<Exercise>,
		allEntries: List<WorkoutEntry>,
		recentEntriesLimit: Int,
		calendarDays: Int,
		progressSessionsPerExercise: Int,
		todaySummary: String,
		yesterdaySummary: String,
		bodyWeightSummary: String? = null,
	): String {
		val weekStart = WorkoutMuscleGroups.weekStartMonday(today)
		val weekEnd = weekStart.plusDays(6)
		val entriesThisWeek =
			allEntries.filter { entry ->
				!entry.performedOn.isBefore(weekStart) && !entry.performedOn.isAfter(today)
			}
		val lastByExerciseId =
			allEntries
				.groupBy { it.exercise.id }
				.mapValues { (_, list) -> list.maxBy { it.performedOn } }
		val doneThisWeekIds =
			entriesThisWeek.map { it.exercise.id }.toSet()

		val recentLimit = recentEntriesLimit.coerceIn(1, 100)

		return buildString {
			appendLine("## Актуальный снимок дневника")
			appendLine(
				"Данные ниже уже в контексте — для плана, статистики и прогресса НЕ вызывай " +
					"get_days / get_progress / list_exercises, если пользователь не просит дату вне календаря.",
			)
			appendLine(nowLine)
			appendLine(
				"Сегодня: $today (${weekdayFmt.format(today)}); " +
					"завтра: ${today.plusDays(1)} (${weekdayFmt.format(today.plusDays(1))}); " +
					"неделя: ${dateFmt.format(weekStart)}–${dateFmt.format(weekEnd)} " +
					"(понедельник–воскресенье)",
			)
			appendLine()
			if (!bodyWeightSummary.isNullOrBlank()) {
				appendLine("### Вес тела")
				appendLine(bodyWeightSummary)
				appendLine()
			}
			appendLine("### Список упражнений (${exercises.size})")
			appendExerciseCatalog(exercises)
			appendLine()
			appendCalendarDays(today, allEntries, calendarDays.coerceIn(1, 31))
			appendLine()
			appendRecentEntries(allEntries, recentLimit)
			appendLine()
			appendExerciseProgress(exercises, allEntries, progressSessionsPerExercise.coerceIn(1, 12))
			appendLine()
			appendWeekSections(today, exercises, entriesThisWeek, doneThisWeekIds, lastByExerciseId)
			appendLine()
			appendLine("### Сегодня")
			appendLine(todaySummary)
			appendLine()
			appendLine("### Вчера")
			appendLine(yesterdaySummary)
			appendLine()
			appendLine("### Все упражнения (последняя сессия — для расчёта весов)")
			appendLastSessionPerExercise(exercises, lastByExerciseId)
		}.trim()
	}

	private fun StringBuilder.appendExerciseCatalog(exercises: List<Exercise>) {
		if (exercises.isEmpty()) {
			appendLine("— упражнений нет")
			return
		}
		for (exercise in exercises.sortedWith(compareBy({ it.muscleGroup }, { it.name }))) {
			appendLine("• «${exercise.name}» (${WorkoutMuscleGroups.labelRu(exercise.muscleGroup)})")
		}
	}

	private fun StringBuilder.appendCalendarDays(
		today: LocalDate,
		allEntries: List<WorkoutEntry>,
		days: Int,
	) {
		appendLine("### Календарь $days дней (новые сверху)")
		val byDay = allEntries.groupBy { it.performedOn }
		var day = today
		repeat(days) {
			val entries = byDay[day].orEmpty().sortedBy { it.createdAt }
			if (entries.isEmpty()) {
				appendLine("• ${dateFmt.format(day)}: —")
			} else {
				val items =
					entries.joinToString("; ") { entry ->
						"«${entry.exercise.name}» ${WorkoutNotation.format(entry)}"
					}
				appendLine("• ${dateFmt.format(day)}: $items")
			}
			day = day.minusDays(1)
		}
	}

	private fun StringBuilder.appendExerciseProgress(
		exercises: List<Exercise>,
		allEntries: List<WorkoutEntry>,
		sessionsPerExercise: Int,
	) {
		appendLine("### История по упражнениям (до $sessionsPerExercise сессий, новые слева)")
		val byExerciseId = allEntries.groupBy { it.exercise.id }
		var withHistory = 0
		for (exercise in exercises.sortedWith(compareBy({ it.muscleGroup }, { it.name }))) {
			val sessions =
				byExerciseId[exercise.id]
					?.sortedByDescending { it.performedOn }
					?.take(sessionsPerExercise)
					.orEmpty()
			if (sessions.isEmpty()) {
				continue
			}
			withHistory++
			val hist =
				sessions.joinToString(" ← ") { session ->
					"${dateFmt.format(session.performedOn)} ${WorkoutNotation.format(session)}"
				}
			appendLine("• «${exercise.name}»: $hist")
		}
		if (withHistory == 0) {
			appendLine("Записей пока нет.")
		}
	}

	private fun StringBuilder.appendRecentEntries(
		allEntries: List<WorkoutEntry>,
		limit: Int,
	) {
		appendLine("### Последние $limit записей дневника")
		val recent = allEntries.take(limit)
		if (recent.isEmpty()) {
			appendLine("Записей пока нет.")
			return
		}
		for (entry in recent) {
			appendLine(
				"• ${dateFmt.format(entry.performedOn)} «${entry.exercise.name}» " +
					"(${WorkoutMuscleGroups.labelRu(entry.exercise.muscleGroup)}): " +
					WorkoutNotation.format(entry),
			)
		}
	}

	private fun StringBuilder.appendWeekSections(
		today: LocalDate,
		exercises: List<Exercise>,
		entriesThisWeek: List<WorkoutEntry>,
		doneThisWeekIds: Set<java.util.UUID>,
		lastByExerciseId: Map<java.util.UUID, WorkoutEntry>,
	) {
		appendLine("### Эта неделя — уже сделано")
		if (entriesThisWeek.isEmpty()) {
			appendLine("На этой неделе записей ещё нет.")
		} else {
			for (entry in entriesThisWeek.sortedWith(compareBy({ it.performedOn }, { it.createdAt }))) {
				appendLine(
					"• ${dateFmt.format(entry.performedOn)} «${entry.exercise.name}» " +
						"(${WorkoutMuscleGroups.labelRu(entry.exercise.muscleGroup)}): " +
						WorkoutNotation.format(entry),
				)
			}
		}
		appendLine()
		appendLine("### Эта неделя — ещё не делали (из списка упражнений)")
		val pending =
			exercises
				.filter { it.id !in doneThisWeekIds }
				.sortedWith(compareBy({ it.muscleGroup }, { it.name }))
		if (pending.isEmpty()) {
			appendLine("Все упражнения из списка уже были на этой неделе.")
		} else {
			for (exercise in pending) {
				val last = lastByExerciseId[exercise.id]
				val lastLine =
					if (last != null) {
						"последний раз ${dateFmt.format(last.performedOn)}: ${WorkoutNotation.format(last)}"
					} else {
						"в дневнике ещё не записывали"
					}
				appendLine(
					"• «${exercise.name}» (${WorkoutMuscleGroups.labelRu(exercise.muscleGroup)}) — $lastLine",
				)
			}
		}
		appendLine()
		appendLine("### Баланс групп мышц на неделе")
		appendMuscleGroupBalance(exercises, doneThisWeekIds)
		appendLine()
		appendLine(
			"Подсказка для плана: чередуй группы (грудь+трицепс, спина+бицепс, ноги, плечи). " +
				"Приоритет — упражнения из «ещё не делали» и группы с 0 сессий на неделе.",
		)
	}

	private fun StringBuilder.appendMuscleGroupBalance(
		exercises: List<Exercise>,
		doneThisWeekIds: Set<java.util.UUID>,
	) {
		val byGroup = exercises.groupBy { it.muscleGroup }
		for ((group, groupExercises) in byGroup.entries.sortedBy { it.key }) {
			val done = groupExercises.count { it.id in doneThisWeekIds }
			val total = groupExercises.size
			val namesDone =
				groupExercises
					.filter { it.id in doneThisWeekIds }
					.joinToString { "«${it.name}»" }
					.ifEmpty { "—" }
			val namesPending =
				groupExercises
					.filter { it.id !in doneThisWeekIds }
					.joinToString { "«${it.name}»" }
					.ifEmpty { "—" }
			appendLine(
				"• ${WorkoutMuscleGroups.labelRu(group)}: $done/$total на неделе | сделано: $namesDone | осталось: $namesPending",
			)
		}
	}

	private fun StringBuilder.appendLastSessionPerExercise(
		exercises: List<Exercise>,
		lastByExerciseId: Map<java.util.UUID, WorkoutEntry>,
	) {
		if (exercises.isEmpty()) {
			appendLine("— упражнений нет")
			return
		}
		for (exercise in exercises.sortedWith(compareBy({ it.muscleGroup }, { it.name }))) {
			val last = lastByExerciseId[exercise.id]
			val line =
				if (last != null) {
					"${dateFmt.format(last.performedOn)} — ${WorkoutNotation.format(last)}"
				} else {
					"нет записей"
				}
			appendLine("• «${exercise.name}» (${WorkoutMuscleGroups.labelRu(exercise.muscleGroup)}): $line")
		}
	}
}
