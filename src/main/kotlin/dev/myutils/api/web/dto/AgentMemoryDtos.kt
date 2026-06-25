package dev.myutils.api.web.dto

import jakarta.validation.constraints.NotBlank

data class CreateAgentFactRequest(
	@field:NotBlank val content: String,
)

data class UpdateAgentFactRequest(
	@field:NotBlank val content: String,
)

data class UpdateMessageExcludedRequest(
	val excludedFromContext: Boolean,
)

data class ResetCompactionResponse(
	val removedSummaries: Int,
)
