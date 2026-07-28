package dev.myutils.api.web.dto

import com.fasterxml.jackson.annotation.JsonIgnoreProperties

@JsonIgnoreProperties(ignoreUnknown = true)
data class ClientEventBatchRequest(
	val events: List<ClientEventRequest> = emptyList(),
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ClientEventRequest(
	val eventId: String? = null,
	val clientId: String? = null,
	val sessionId: String? = null,
	val pageViewId: String? = null,
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
	val screenWidth: Int? = null,
	val screenHeight: Int? = null,
	val sequence: Int? = null,
	val elapsedMs: Long? = null,
	val sincePreviousMs: Long? = null,
	val durationMs: Long? = null,
	val changed: Boolean? = null,
	val fieldState: String? = null,
	val webdriver: Boolean? = null,
	val language: String? = null,
	val platform: String? = null,
	val hardwareConcurrency: Int? = null,
	val maxTouchPoints: Int? = null,
)
