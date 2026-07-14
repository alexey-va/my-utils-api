package dev.myutils.api.web.dto

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
)
