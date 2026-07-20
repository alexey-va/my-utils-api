package dev.myutils.api.service

/**
 * Парсит запись тренировки одной строкой — для LLM достаточно передать notation.
 *
 * Примеры:
 * - `70 3*10/12` — 3 рабочих + МАХ (4 подхода)
 * - `70 10/12` — то же shorthand: 3×10 + МАХ 12 (не два подхода)
 * - `70 10/10` — ровно два подхода по 10
 * - `70 7/7/7` — три подхода по 7
 * - `70/75/80 10/10/10` — разный вес на каждый подход
 */
object WorkoutNotationParser {
	data class Parsed(
		val weightKg: Int,
		val weights: List<Int>?,
		val reps: List<Int>,
		val setCount: Int,
		val repsPerSet: Int,
		val maxReps: Int,
	)

	fun parse(raw: String): Parsed {
		val notation = raw.trim().replace(Regex("\\s+"), " ")
		require(notation.isNotEmpty()) { "Пустая notation" }

		val classic = Regex("""^(\d+)\s+(\d+)\*(\d+)/(\d+)$""").matchEntire(notation)
		if (classic != null) {
			val weight = classic.groupValues[1].toInt()
			val workingSets = classic.groupValues[2].toInt()
			val repsPerSet = classic.groupValues[3].toInt()
			val maxReps = classic.groupValues[4].toInt()
			require(workingSets >= 1) { "Число рабочих подходов ≥ 1" }
			val reps = List(workingSets) { repsPerSet } + maxReps
			return toParsed(weight, null, reps)
		}

		val variable =
			Regex("""^([\d./]+)\s+([\d/,]+)$""").matchEntire(notation)
				?: throw IllegalArgumentException(
					"Не понял notation «$raw». Примеры: 70 3*10/12, 70 10/12, 70 10/10, 70 7/7/7, 70/75/80 10/10/10",
				)

		val left = parseNumberList(variable.groupValues[1])
		val right = expandRepsShorthand(parseNumberList(variable.groupValues[2]))

		return when {
			left.size > 1 && right.size > 1 -> {
				require(left.size == right.size) {
					"Число весов (${left.size}) должно совпадать с числом подходов (${right.size})"
				}
				toParsed(left.max(), left, right)
			}
			left.size == 1 -> toParsed(left.first(), null, right)
			else ->
				throw IllegalArgumentException(
					"Не понял notation «$raw». Укажи вес и повторы: 70 3*10/12, 70 10/12 или 70/75/80 10/10/10",
				)
		}
	}

	/**
	 * `A/B` с разными числами — shorthand дневника: 3×A + МАХ B.
	 * `A/A` — ровно два одинаковых подхода. Три и больше — как есть.
	 */
	internal fun expandRepsShorthand(reps: List<Int>): List<Int> {
		if (reps.size == 2 && reps[0] != reps[1]) {
			val working = reps[0]
			val maxReps = reps[1]
			return listOf(working, working, working, maxReps)
		}
		return reps
	}

	private fun parseNumberList(raw: String): List<Int> =
		raw.split(Regex("[/,]")).map { part ->
			part.trim().toIntOrNull()
				?: throw IllegalArgumentException("Некорректное число: «$part»")
		}.also { nums ->
			require(nums.isNotEmpty()) { "Список чисел пуст" }
			require(nums.all { it >= 1 }) { "Каждое значение ≥ 1" }
		}

	private fun toParsed(
		weightKg: Int,
		weights: List<Int>?,
		reps: List<Int>,
	): Parsed {
		val normalized = WorkoutSetReps.normalize(setCount = reps.size, repsPerSet = reps.min(), maxReps = reps.max(), setReps = reps)
		return Parsed(
			weightKg = weightKg,
			weights = weights,
			reps = normalized.reps,
			setCount = normalized.setCount,
			repsPerSet = normalized.repsPerSet,
			maxReps = normalized.maxReps,
		)
	}
}
