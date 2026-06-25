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

data class ResetCompactionResponse(
	val removedSummaries: Int,
)
