package dev.myutils.api.agent

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.openrouter.ToolDefinition
import dev.myutils.api.openrouter.ToolFunction
import org.springframework.stereotype.Component

@Component
class WorkoutAgentTools(
	private val objectMapper: ObjectMapper,
) {
	fun definitions(): List<ToolDefinition> =
		listOf(
			tool(
				"list_exercises",
				"Список всех упражнений в дневнике (id, название, группа мышц).",
				"""{"type":"object","properties":{}}""",
			),
			tool(
				"create_exercise",
				"Создать новое упражнение, если его ещё нет. Для гантелей укажи «гантели» в названии.",
				"""
				{
				  "type": "object",
				  "properties": {
				    "name": {"type": "string", "description": "Название упражнения"},
				    "muscle_group": {
				      "type": "string",
				      "description": "chest, back, legs, shoulders, arms, core, other"
				    }
				  },
				  "required": ["name"]
				}
				""".trimIndent(),
			),
			tool(
				"log_workout",
				"""Записать или обновить тренировку за день. Формат тренера: вес 3*X/МАХ — set_count=3, reps_per_set=X (8–10), max_reps=МАХ (4-й подход). weight_kg — полный вес штанги (гриф+блинья); для гантелей вес одной гантели, в названии упражнения указать «гантели».""",
				"""
				{
				  "type": "object",
				  "properties": {
				    "exercise_name": {"type": "string"},
				    "performed_on": {"type": "string", "description": "YYYY-MM-DD, по умолчанию сегодня (Москва)"},
				    "weight_kg": {"type": "integer", "description": "Вес в кг"},
				    "set_count": {"type": "integer", "description": "Обычно 3 (первые три подхода)"},
				    "reps_per_set": {"type": "integer", "description": "X: повторы в подходах 1–3 (8–10)"},
				    "max_reps": {"type": "integer", "description": "МАХ: повторы в 4-м подходе"}
				  },
				  "required": ["exercise_name", "weight_kg", "reps_per_set", "max_reps"]
				}
				""".trimIndent(),
			),
			tool(
				"get_exercise_progress",
				"Прогресс и статистика по упражнению — для ответов на вопросы и сверки после записи.",
				"""
				{
				  "type": "object",
				  "properties": {
				    "exercise_name": {"type": "string"},
				    "recent_sessions": {"type": "integer", "description": "Сколько последних сессий показать, по умолчанию 6"}
				  },
				  "required": ["exercise_name"]
				}
				""".trimIndent(),
			),
			tool(
				"get_day_summary",
				"Что записано за день — для сверки и ответов «что сегодня сделал».",
				"""
				{
				  "type": "object",
				  "properties": {
				    "performed_on": {"type": "string", "description": "YYYY-MM-DD, по умолчанию сегодня"}
				  }
				}
				""".trimIndent(),
			),
		)

	private fun tool(
		name: String,
		description: String,
		parametersJson: String,
	): ToolDefinition =
		ToolDefinition(
			function =
				ToolFunction(
					name = name,
					description = description,
					parameters = objectMapper.readTree(parametersJson),
				),
		)
}
