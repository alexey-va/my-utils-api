package dev.myutils.api.web.dto

import com.fasterxml.jackson.annotation.JsonIgnore
import jakarta.validation.constraints.AssertTrue
import jakarta.validation.constraints.NotBlank

data class CreateAgentFactRequest(
	@field:NotBlank val content: String,
	val confidence: Double? = null,
)

data class UpdateAgentFactRequest(
	@field:NotBlank val content: String,
	val confidence: Double? = null,
)

data class UpdateMessageExcludedRequest(
	val excludedFromContext: Boolean,
)

data class CreateAgentMessageRequest(
	@field:NotBlank val role: String,
	val content: String = "",
	val images: List<String>? = null,
)

data class AgentChatTurnRequest(
	val content: String = "",
	val images: List<String>? = null,
) {
	@get:JsonIgnore
	@get:AssertTrue(message = "Нужен текст или хотя бы одно изображение.")
	val hasPayload: Boolean
		get() = content.isNotBlank() || !images.isNullOrEmpty()
}
