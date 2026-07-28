package dev.myutils.api.web.dto

import com.fasterxml.jackson.annotation.JsonIgnoreProperties

@JsonIgnoreProperties(ignoreUnknown = true)
data class ClientEventBatchRequest(
	val events: List<ClientEventRequest> = emptyList(),
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ClientEventRequest(
	val eventId: String? = null,
	val sessionId: String? = null,
	val occurredAt: String? = null,
	val type: String? = null,
	val page: String? = null,
	val uiMode: String? = null,
	val targetTag: String? = null,
	val targetKey: String? = null,
	val targetType: String? = null,
	val detail: String? = null,
	val viewportWidth: Int? = null,
	val viewportHeight: Int? = null,
)
