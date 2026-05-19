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
				"rename_exercise",
				"Переименовать упражнение в дневнике (записи сохраняются). Можно сменить группу мышц.",
				"""
				{
				  "type": "object",
				  "properties": {
				    "current_name": {"type": "string", "description": "Текущее название в дневнике"},
				    "new_name": {"type": "string", "description": "Новое название"},
				    "muscle_group": {
				      "type": "string",
				      "description": "Опционально: chest, back, legs, shoulders, arms, core, other"
				    }
				  },
				  "required": ["current_name", "new_name"]
				}
				""".trimIndent(),
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
				"delete_workout",
				"Удалить запись за день (упражнение + дата).",
				"""
				{
				  "type": "object",
				  "properties": {
				    "exercise_name": {"type": "string"},
				    "performed_on": {"type": "string", "description": "YYYY-MM-DD, по умолчанию сегодня"}
				  },
				  "required": ["exercise_name"]
				}
				""".trimIndent(),
			),
			tool(
				"log_workout",
				"Записать/обновить за день: вес 3*X/МАХ → set_count=3, reps_per_set=X, max_reps=МАХ, weight_kg полный.",
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
				"Прогресс по упражнению (если нет в снимке).",
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
				"Записи за день (если нет в снимке).",
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
