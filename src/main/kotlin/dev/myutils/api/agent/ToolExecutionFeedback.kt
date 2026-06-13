package dev.myutils.api.agent

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.databind.node.ObjectNode

object ToolExecutionFeedback {
	private val objectMapper = ObjectMapper()

	fun success(value: String): String = value

	fun failure(
		error: String,
		hint: String? = null,
	): String {
		val node: ObjectNode = objectMapper.createObjectNode()
		node.put("result", false)
		node.put("error", error)
		if (!hint.isNullOrBlank()) {
			node.put("hint", hint.trim())
		}
		return objectMapper.writeValueAsString(node)
	}

	fun isFailure(result: String): Boolean {
		val trimmed = result.trim()
		if (!trimmed.startsWith("{")) {
			return false
		}
		return try {
			val node = objectMapper.readTree(trimmed)
			node.isObject && node.path("result").asBoolean(false).not() && node.has("result")
		} catch (_: Exception) {
			false
		}
	}
}
