package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.domain.AgentTestSandboxState
import dev.myutils.api.domain.AgentTestSandboxStateRepository
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.service.WorkoutNotationParser
import dev.myutils.api.service.WorkoutSetReps
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import java.math.BigDecimal
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.util.UUID

@Service
class AgentTestSandboxService(
	private val repository: AgentTestSandboxStateRepository,
	private val objectMapper: ObjectMapper,
) {
	fun isSandboxChatId(memoryChatId: Long): Boolean = memoryChatId in MEMORY_CHAT_ID_RANGE

	@Transactional
	fun create(memoryChatId: Long) {
		require(isSandboxChatId(memoryChatId)) { "Chat id $memoryChatId is outside the reserved sandbox range." }
		if (!repository.existsById(memoryChatId)) {
			repository.save(AgentTestSandboxState(memoryChatId = memoryChatId, stateJson = encode(SandboxState())))
		}
	}

	@Transactional
	fun reset(memoryChatId: Long) {
		val row = locked(memoryChatId)
		row.stateJson = encode(SandboxState())
		row.updatedAt = Instant.now()
	}

	@Transactional(readOnly = true)
	fun buildSnapshot(memoryChatId: Long): String {
		val state = decode(repository.findById(memoryChatId).orElseThrow().stateJson)
		val exercises =
			if (state.exercises.isEmpty()) {
				"— упражнений нет"
			} else {
				state.exercises.sortedBy { it.name.lowercase() }.joinToString("\n") {
					"• «${it.name}» (${it.muscleGroup}) [sandbox_id=${it.id}]"
				}
			}
		val workouts =
			if (state.workouts.isEmpty()) {
				"— записей нет"
			} else {
				state.workouts
					.sortedWith(compareByDescending<SandboxWorkout> { it.performedOn }.thenBy { it.exerciseName })
					.take(20)
					.joinToString("\n") {
						"• ${it.performedOn}: ${it.exerciseName} — " +
							WorkoutSetReps.displayRu(it.weightKg, it.reps, it.weights)
					}
			}
		val weights =
			if (state.bodyWeights.isEmpty()) {
				"— замеров нет"
			} else {
				state.bodyWeights.sortedByDescending { it.performedOn }.take(10).joinToString("\n") {
					"• ${it.performedOn}: ${formatNumber(it.weightKg)} кг"
				}
			}
		return """
			## ИЗОЛИРОВАННЫЙ SANDBOX
			Это отдельный тестовый контекст. В нём нет реальных Workout-данных, Telegram-доставки
			или production-уведомлений. Все изменения tools остаются только внутри этого тестового чата.

			### Упражнения
			$exercises

			### Тренировки
			$workouts

			### Вес тела
			$weights
		""".trimIndent()
	}

	@Transactional(readOnly = true)
	fun formatFacts(memoryChatId: Long): String {
		val facts = decode(repository.findById(memoryChatId).orElseThrow().stateJson).facts
		if (facts.isEmpty()) {
			return "Изолированные sandbox-факты: —"
		}
		return buildString {
			appendLine("Изолированные sandbox-факты:")
			facts.forEach { appendLine("• [${it.id}] ${it.content}") }
		}.trim()
	}

	@Transactional
	fun executeTool(
		memoryChatId: Long,
		toolName: String,
		args: Map<String, String?>,
	): String {
		val row = locked(memoryChatId)
		val state = decode(row.stateJson)
		val result =
			try {
				when (toolName) {
					"list_exercises" -> listExercises(state)
					"create_exercise" -> createExercise(state, args)
					"rename_exercise" -> renameExercise(state, args)
					"log_workout" -> logWorkout(state, args)
					"delete_workout" -> deleteWorkout(state, args)
					"get_exercise_progresses", "get_progress" -> getProgress(state, args)
					"get_day_summaries", "get_days" -> getDays(state, args)
					"remember_fact" -> rememberFact(state, args.require("content"))
					"forget_fact" -> forgetFact(state, args.require("fact_id"))
					"manage_user_fact" -> manageFact(state, args)
					"log_body_weight" -> logBodyWeight(state, args)
					"get_body_weight" -> getBodyWeight(state, args)
					"send_notification" -> recordNotification(state, "sent", args.require("message"))
					"schedule_notification" ->
						recordNotification(
							state,
							"scheduled:${args.require("deliver_at")}",
							args.require("message"),
						)
					"cancel_notification" -> cancelNotification(state, args.require("workflow_id"))
					"send_rich_message" ->
						"SANDBOX: сообщение сохранено только как tool result; в Telegram ничего не отправлено."
					"send_progress_chart" ->
						"SANDBOX: график «${args.require("exercise_name")}» не отправлялся наружу.\n" +
							getProgress(state, mapOf("exercise" to args.require("exercise_name")))
					"estimate_1rm" -> estimateOneRm(state, args)
					else -> ToolExecutionFeedback.failure("Неизвестный sandbox-инструмент: $toolName")
				}
			} catch (ex: IllegalArgumentException) {
				ToolExecutionFeedback.failure(ex.message ?: "Invalid sandbox tool arguments")
			}
		row.stateJson = encode(state)
		row.updatedAt = Instant.now()
		return result
	}

	private fun listExercises(state: SandboxState): String =
		if (state.exercises.isEmpty()) {
			"В SANDBOX упражнений пока нет."
		} else {
			state.exercises.sortedBy { it.name.lowercase() }.joinToString("\n") {
				"• ${it.name} (${it.muscleGroup}) [sandbox_id=${it.id}]"
			}
		}

	private fun createExercise(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val name = args.require("name")
		require(state.exercises.none { it.name.equals(name, ignoreCase = true) }) {
			"В SANDBOX уже есть упражнение «$name»."
		}
		val exercise =
			SandboxExercise(
				id = UUID.randomUUID().toString(),
				name = name,
				muscleGroup = args.optional("muscle_group") ?: "other",
			)
		state.exercises.add(exercise)
		return "SANDBOX: создано упражнение «${exercise.name}» (${exercise.muscleGroup})."
	}

	private fun renameExercise(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val exercise = requireExercise(state, args.require("current_name"))
		val previous = exercise.name
		exercise.name = args.require("new_name")
		args.optional("muscle_group")?.let { exercise.muscleGroup = it }
		state.workouts.filter { it.exerciseId == exercise.id }.forEach { it.exerciseName = exercise.name }
		return "SANDBOX: «$previous» переименовано в «${exercise.name}»."
	}

	private fun logWorkout(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val exercise = requireExercise(state, args.require("exercise_name"))
		val date = parseDate(args.optional("performed_on") ?: args.optional("date")) ?: today()
		val parsed = WorkoutNotationParser.parse(resolveNotation(args))
		state.workouts.removeIf { it.exerciseId == exercise.id && it.performedOn == date }
		state.workouts.add(
			SandboxWorkout(
				exerciseId = exercise.id,
				exerciseName = exercise.name,
				performedOn = date,
				weightKg = parsed.weightKg,
				reps = parsed.reps,
				weights = parsed.weights,
			),
		)
		return "SANDBOX: записано ${exercise.name}, $date — " +
			WorkoutSetReps.displayRu(parsed.weightKg, parsed.reps, parsed.weights)
	}

	private fun deleteWorkout(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val exercise = requireExercise(state, args.require("exercise_name"))
		val date = parseDate(args.optional("performed_on") ?: args.optional("date")) ?: today()
		require(state.workouts.removeIf { it.exerciseId == exercise.id && it.performedOn == date }) {
			"В SANDBOX нет записи «${exercise.name}» за $date."
		}
		return "SANDBOX: удалена запись ${exercise.name} за $date."
	}

	private fun getProgress(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val names =
			(args.optional("exercises") ?: args.require("exercise"))
				.split(",")
				.map { it.trim() }
				.filter { it.isNotEmpty() }
		val limit = args.optional("recent_sessions")?.toIntOrNull()?.coerceIn(1, 20) ?: 6
		return names.joinToString("\n\n") { name ->
			val exercise = requireExercise(state, name)
			val rows =
				state.workouts
					.filter { it.exerciseId == exercise.id }
					.sortedByDescending { it.performedOn }
					.take(limit)
			if (rows.isEmpty()) {
				"«${exercise.name}» — в SANDBOX записей пока нет."
			} else {
				buildString {
					appendLine("«${exercise.name}» — SANDBOX-прогресс:")
					rows.forEach {
						appendLine("• ${it.performedOn}: ${WorkoutSetReps.displayRu(it.weightKg, it.reps, it.weights)}")
					}
				}.trim()
			}
		}
	}

	private fun getDays(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val dates = resolveDates(args)
		return dates.joinToString("\n\n") { date ->
			val rows = state.workouts.filter { it.performedOn == date }.sortedBy { it.exerciseName }
			if (rows.isEmpty()) {
				"За $date в SANDBOX записей нет."
			} else {
				buildString {
					appendLine("SANDBOX-тренировка за $date:")
					rows.forEach {
						appendLine("• ${it.exerciseName}: ${WorkoutSetReps.displayRu(it.weightKg, it.reps, it.weights)}")
					}
				}.trim()
			}
		}
	}

	private fun rememberFact(
		state: SandboxState,
		content: String,
	): String {
		val fact = SandboxFact(UUID.randomUUID().toString(), content)
		state.facts.add(fact)
		return "SANDBOX: факт сохранён [${fact.id}]."
	}

	private fun forgetFact(
		state: SandboxState,
		factId: String,
	): String {
		require(state.facts.removeIf { it.id == factId }) { "Sandbox-факт $factId не найден." }
		return "SANDBOX: факт удалён."
	}

	private fun manageFact(
		state: SandboxState,
		args: Map<String, String?>,
	): String =
		when (args.require("action").lowercase()) {
			"remember" -> rememberFact(state, args.require("content"))
			"forget" -> forgetFact(state, args.require("fact_id"))
			"update" -> {
				val fact = state.facts.firstOrNull { it.id == args.require("fact_id") }
					?: throw IllegalArgumentException("Sandbox-факт не найден.")
				fact.content = args.require("content")
				"SANDBOX: факт обновлён."
			}
			else -> throw IllegalArgumentException("Неизвестный action.")
		}

	private fun logBodyWeight(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val value =
			args.require("weight_kg").replace(',', '.').toBigDecimalOrNull()
				?: throw IllegalArgumentException("Неверный вес.")
		val date = parseDate(args.optional("performed_on") ?: args.optional("date")) ?: today()
		state.bodyWeights.removeIf { it.performedOn == date }
		state.bodyWeights.add(SandboxBodyWeight(date, value))
		return "SANDBOX: вес ${formatNumber(value)} кг сохранён за $date."
	}

	private fun getBodyWeight(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val days = args.optional("recent_days")?.toLongOrNull()?.coerceIn(1, 90) ?: 14
		val from = today().minusDays(days - 1)
		val rows = state.bodyWeights.filter { !it.performedOn.isBefore(from) }.sortedByDescending { it.performedOn }
		return if (rows.isEmpty()) {
			"В SANDBOX замеров веса пока нет."
		} else {
			rows.joinToString("\n") { "• ${it.performedOn}: ${formatNumber(it.weightKg)} кг" }
		}
	}

	private fun recordNotification(
		state: SandboxState,
		status: String,
		message: String,
	): String {
		val id = "sandbox-${UUID.randomUUID()}"
		state.notifications.add(SandboxNotification(id, status, message))
		return "SANDBOX: уведомление $id сохранено локально; наружу ничего не отправлено."
	}

	private fun cancelNotification(
		state: SandboxState,
		workflowId: String,
	): String {
		val row = state.notifications.firstOrNull { it.id == workflowId }
			?: throw IllegalArgumentException("Sandbox-уведомление $workflowId не найдено.")
		row.status = "cancelled"
		return "SANDBOX: уведомление $workflowId отменено локально."
	}

	private fun estimateOneRm(
		state: SandboxState,
		args: Map<String, String?>,
	): String {
		val exercise = requireExercise(state, args.require("exercise_name"))
		val requestedDate = parseDate(args.optional("performed_on") ?: args.optional("date"))
		val row =
			state.workouts
				.filter { it.exerciseId == exercise.id && (requestedDate == null || it.performedOn == requestedDate) }
				.maxByOrNull { it.performedOn }
				?: throw IllegalArgumentException("В SANDBOX нет записей по «${exercise.name}».")
		val bestReps = row.reps.maxOrNull() ?: 1
		val estimate = row.weightKg.toDouble() * (1.0 + bestReps / 30.0)
		return "SANDBOX: оценка 1ПМ для «${exercise.name}» — ${"%.1f".format(estimate)} кг. " +
			"Изображение и Telegram-сообщение не отправлялись."
	}

	private fun requireExercise(
		state: SandboxState,
		name: String,
	): SandboxExercise {
		val matches =
			state.exercises.filter {
				it.name.equals(name, ignoreCase = true) ||
					it.name.contains(name, ignoreCase = true) ||
					name.contains(it.name, ignoreCase = true)
			}
		return when (matches.size) {
			1 -> matches.single()
			0 -> throw IllegalArgumentException("В SANDBOX нет упражнения «$name».")
			else -> throw IllegalArgumentException("Неоднозначное sandbox-упражнение «$name».")
		}
	}

	private fun resolveNotation(args: Map<String, String?>): String {
		args.optional("notation")?.let { return it }
		val weight = args.optional("weight_kg")?.toIntOrNull()
			?: throw IllegalArgumentException("Нужно поле notation.")
		args.optional("set_reps")?.let { return "$weight $it" }
		val setCount = args.optional("set_count")?.toIntOrNull() ?: 3
		val reps = args.optional("reps_per_set")?.toIntOrNull()
			?: throw IllegalArgumentException("Нужно поле notation.")
		val max = args.optional("max_reps")?.toIntOrNull()
			?: throw IllegalArgumentException("Нужно поле notation.")
		return "$weight $setCount*$reps/$max"
	}

	private fun resolveDates(args: Map<String, String?>): List<LocalDate> {
		args.optional("days")?.let { raw ->
			return raw.split(",").map { parseDate(it) ?: throw IllegalArgumentException("Неверная дата.") }.distinct()
		}
		val from = parseDate(args.optional("from"))
		val to = parseDate(args.optional("to"))
		if (from == null && to == null) return listOf(today())
		require(from != null && to != null) { "Для интервала нужны from и to." }
		require(!from.isAfter(to)) { "from не может быть позже to." }
		val result = generateSequence(from) { if (it.isBefore(to)) it.plusDays(1) else null }.toList()
		require(result.size <= 31) { "Интервал слишком большой." }
		return result
	}

	private fun locked(memoryChatId: Long): AgentTestSandboxState =
		repository.findForUpdate(memoryChatId).orElseThrow {
			IllegalArgumentException("Sandbox test chat $memoryChatId не найден.")
		}

	private fun decode(raw: String): SandboxState =
		runCatching { objectMapper.readValue<SandboxState>(raw) }.getOrDefault(SandboxState())

	private fun encode(state: SandboxState): String = objectMapper.writeValueAsString(state)

	private fun parseDate(raw: String?): LocalDate? =
		raw?.trim()?.takeIf { it.isNotEmpty() }?.let {
			runCatching { LocalDate.parse(it) }
				.getOrElse { throw IllegalArgumentException("Неверная дата: $raw (YYYY-MM-DD).") }
		}

	private fun today(): LocalDate = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))

	private fun formatNumber(value: BigDecimal): String = value.stripTrailingZeros().toPlainString()

	private fun Map<String, String?>.require(key: String): String =
		optional(key) ?: throw IllegalArgumentException("Нужно поле $key")

	private fun Map<String, String?>.optional(key: String): String? =
		this[key]?.trim()?.takeIf { it.isNotEmpty() && !it.equals("null", ignoreCase = true) }

	private companion object {
		val MEMORY_CHAT_ID_RANGE = -9_000_000_000_000_000L..-8_000_000_000_000_000L
	}
}

private data class SandboxState(
	val exercises: MutableList<SandboxExercise> = mutableListOf(),
	val workouts: MutableList<SandboxWorkout> = mutableListOf(),
	val bodyWeights: MutableList<SandboxBodyWeight> = mutableListOf(),
	val facts: MutableList<SandboxFact> = mutableListOf(),
	val notifications: MutableList<SandboxNotification> = mutableListOf(),
)

private data class SandboxExercise(
	val id: String,
	var name: String,
	var muscleGroup: String,
)

private data class SandboxWorkout(
	val exerciseId: String,
	var exerciseName: String,
	val performedOn: LocalDate,
	val weightKg: Int,
	val reps: List<Int>,
	val weights: List<Int>? = null,
)

private data class SandboxBodyWeight(
	val performedOn: LocalDate,
	val weightKg: BigDecimal,
)

private data class SandboxFact(
	val id: String,
	var content: String,
)

private data class SandboxNotification(
	val id: String,
	var status: String,
	val message: String,
)
