package dev.myutils.api.web.dto

import com.fasterxml.jackson.databind.JsonNode

data class StepsIngestResponse(
	val ok: Boolean,
	val received: JsonNode?,
)
