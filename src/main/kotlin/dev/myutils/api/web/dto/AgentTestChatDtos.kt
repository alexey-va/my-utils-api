package dev.myutils.api.web.dto

import jakarta.validation.constraints.NotBlank
import jakarta.validation.constraints.Size

data class CreateAgentTestChatRequest(
	@field:NotBlank
	@field:Size(max = 120)
	val title: String,
)

data class RenameAgentTestChatRequest(
	@field:NotBlank
	@field:Size(max = 120)
	val title: String,
)
