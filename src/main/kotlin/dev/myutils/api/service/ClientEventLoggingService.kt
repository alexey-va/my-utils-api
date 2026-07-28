package dev.myutils.api.service

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.web.dto.ClientEventBatchRequest
import dev.myutils.api.web.dto.ClientEventRequest
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Service
import java.time.Instant

@Service
class ClientEventLoggingService(
	private val objectMapper: ObjectMapper,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun logBatch(
		body: String?,
		origin: String?,
	): Int {
		val events = ClientEventSanitizer.parse(body, objectMapper)
		val safeOrigin = ClientEventSanitizer.safeText(origin, MAX_ORIGIN_LENGTH)

		events.forEach { event ->
			log
				.atInfo()
				.addKeyValue("event_type", "client_event")
				.addKeyValue("client_app", "route-planner")
				.addKeyValue("client_event_type", event.type)
				.addKeyValue("client_event_id", event.eventId)
				.addKeyValue("client_session_id", event.sessionId)
				.addKeyValue("client_occurred_at", event.occurredAt)
				.addKeyValue("client_page", event.page)
				.addKeyValue("client_ui_mode", event.uiMode)
				.addKeyValue("client_target_tag", event.targetTag)
				.addKeyValue("client_target_key", event.targetKey)
				.addKeyValue("client_target_type", event.targetType)
				.addKeyValue("client_detail", event.detail)
				.addKeyValue("client_viewport_width", event.viewportWidth)
				.addKeyValue("client_viewport_height", event.viewportHeight)
				.addKeyValue("client_origin", safeOrigin)
				.log("Route planner client event")
		}

		return events.size
	}

	private companion object {
		const val MAX_ORIGIN_LENGTH = 200
	}
}

internal object ClientEventSanitizer {
	private const val MAX_BODY_LENGTH = 64 * 1024
	private const val MAX_BATCH_SIZE = 25
	private const val MAX_TEXT_LENGTH = 160
	private const val MAX_PAGE_LENGTH = 120
	private const val MAX_TYPE_LENGTH = 64
	private const val MAX_SMALL_FIELD_LENGTH = 40
	private const val MAX_VIEWPORT_SIZE = 20_000
	private val TYPE_PATTERN = Regex("[a-z][a-z0-9_.-]{0,63}")
	private val TOKEN_PATTERN = Regex("[a-zA-Z0-9_-]{1,160}")

	fun parse(
		body: String?,
		objectMapper: ObjectMapper,
	): List<SanitizedClientEvent> {
		if (body.isNullOrBlank() || body.length > MAX_BODY_LENGTH) return emptyList()

		val request =
			runCatching {
				objectMapper.readValue(body, ClientEventBatchRequest::class.java)
			}.getOrNull() ?: return emptyList()

		return request.events
			.take(MAX_BATCH_SIZE)
			.mapNotNull(::sanitize)
	}

	fun safeText(
		value: String?,
		maxLength: Int = MAX_TEXT_LENGTH,
	): String? {
		val normalized =
			value
				?.replace(CONTROL_CHARACTERS, " ")
				?.trim()
				.orEmpty()
		return normalized.take(maxLength).ifBlank { null }
	}

	private fun sanitize(event: ClientEventRequest): SanitizedClientEvent? {
		val type = safeText(event.type, MAX_TYPE_LENGTH)?.takeIf(TYPE_PATTERN::matches) ?: return null
		val page =
			safeText(event.page?.substringBefore('?'), MAX_PAGE_LENGTH)
				?.takeIf { it.startsWith("/") }
				?: "/"

		return SanitizedClientEvent(
			eventId = safeToken(event.eventId),
			sessionId = safeToken(event.sessionId),
			occurredAt = parseInstant(event.occurredAt),
			type = type,
			page = page,
			uiMode = safeText(event.uiMode, MAX_SMALL_FIELD_LENGTH),
			targetTag = safeText(event.targetTag, MAX_SMALL_FIELD_LENGTH),
			targetKey = safeText(event.targetKey),
			targetType = safeText(event.targetType, MAX_SMALL_FIELD_LENGTH),
			detail = safeText(event.detail),
			viewportWidth = event.viewportWidth?.takeIf { it in 0..MAX_VIEWPORT_SIZE },
			viewportHeight = event.viewportHeight?.takeIf { it in 0..MAX_VIEWPORT_SIZE },
		)
	}

	private fun safeToken(value: String?): String? =
		safeText(value)?.takeIf(TOKEN_PATTERN::matches)

	private fun parseInstant(value: String?): String? =
		runCatching { Instant.parse(value).toString() }.getOrNull()

	private val CONTROL_CHARACTERS = Regex("[\\u0000-\\u001f\\u007f]")
}

internal data class SanitizedClientEvent(
	val eventId: String?,
	val sessionId: String?,
	val occurredAt: String?,
	val type: String,
	val page: String,
	val uiMode: String?,
	val targetTag: String?,
	val targetKey: String?,
	val targetType: String?,
	val detail: String?,
	val viewportWidth: Int?,
	val viewportHeight: Int?,
)
