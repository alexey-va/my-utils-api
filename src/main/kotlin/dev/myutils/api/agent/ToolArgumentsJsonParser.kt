package dev.myutils.api.agent

import com.fasterxml.jackson.core.JsonParseException
import com.fasterxml.jackson.databind.ObjectMapper

object ToolArgumentsJsonParser {
	private val UNQUOTED_ISO_DATE =
		Regex("""(:\s*)(\d{4,5}-\d{2}-\d{2})(\s*[,}])""")

	sealed interface ParseResult {
		data class Ok(
			val args: Map<String, String?>,
		) : ParseResult

		data class Error(
			val message: String,
		) : ParseResult
	}

	fun parse(
		objectMapper: ObjectMapper,
		argumentsJson: String,
	): ParseResult {
		if (argumentsJson.isBlank()) {
			return ParseResult.Ok(emptyMap())
		}
		val trimmed = argumentsJson.trim()
		val candidates =
			listOf(
				trimmed,
				sanitize(trimmed),
			).distinct()
		var lastError: String? = null
		for (candidate in candidates) {
			when (val parsed = tryParse(objectMapper, candidate)) {
				is ParseResult.Ok -> return parsed
				is ParseResult.Error -> lastError = parsed.message
			}
		}
		return ParseResult.Error(
			lastError
				?: "Не удалось разобрать аргументы. Передавай даты в кавычках, формат YYYY-MM-DD.",
		)
	}

	private fun sanitize(raw: String): String =
		UNQUOTED_ISO_DATE.replace(raw) { match ->
			"${match.groupValues[1]}\"${match.groupValues[2]}\"${match.groupValues[3]}"
		}

	private fun tryParse(
		objectMapper: ObjectMapper,
		raw: String,
	): ParseResult {
		return try {
			val node = objectMapper.readTree(raw)
			if (!node.isObject) {
				return ParseResult.Error("Аргументы инструмента должны быть JSON-объектом.")
			}
			ParseResult.Ok(
				node
					.properties()
					.associate { (key, value) ->
						key to
							when {
								value.isNull -> null
								value.isTextual -> value.asText()
								value.isNumber -> value.asText()
								value.isBoolean -> value.asText()
								else -> value.toString()
							}
					},
			)
		} catch (ex: JsonParseException) {
			ParseResult.Error("Невалидный JSON аргументов: ${ex.originalMessage}")
		} catch (ex: Exception) {
			ParseResult.Error(ex.message ?: "Невалидный JSON аргументов.")
		}
	}
}
