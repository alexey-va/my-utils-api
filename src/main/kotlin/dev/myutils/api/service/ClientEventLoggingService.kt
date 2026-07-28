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
		requestContext: ClientRequestContext,
	): Int {
		val events = ClientEventSanitizer.parse(body, objectMapper)
		val safeContext = requestContext.sanitized()

		events.forEach { event ->
			log
				.atInfo()
				.addKeyValue("event_type", "client_event")
				.addKeyValue("client_app", "route-planner")
				.addKeyValue("client_event_type", event.type)
				.addKeyValue("client_event_id", event.eventId)
				.addKeyValue("client_id", event.clientId)
				.addKeyValue("client_session_id", event.sessionId)
				.addKeyValue("client_page_view_id", event.pageViewId)
				.addKeyValue("client_occurred_at", event.occurredAt)
				.addKeyValue("client_sequence", event.sequence)
				.addKeyValue("client_elapsed_ms", event.elapsedMs)
				.addKeyValue("client_since_previous_ms", event.sincePreviousMs)
				.addKeyValue("client_page", event.page)
				.addKeyValue("client_ui_mode", event.uiMode)
				.addKeyValue("client_target_tag", event.targetTag)
				.addKeyValue("client_target_key", event.targetKey)
				.addKeyValue("client_target_type", event.targetType)
				.addKeyValue("client_detail", event.detail)
				.addKeyValue("client_viewport_width", event.viewportWidth)
				.addKeyValue("client_viewport_height", event.viewportHeight)
				.addKeyValue("client_screen_width", event.screenWidth)
				.addKeyValue("client_screen_height", event.screenHeight)
				.addKeyValue("client_duration_ms", event.durationMs)
				.addKeyValue("client_changed", event.changed)
				.addKeyValue("client_field_state", event.fieldState)
				.addKeyValue("client_webdriver", event.webdriver)
				.addKeyValue("client_language", event.language)
				.addKeyValue("client_platform", event.platform)
				.addKeyValue("client_hardware_concurrency", event.hardwareConcurrency)
				.addKeyValue("client_max_touch_points", event.maxTouchPoints)
				.addKeyValue("client_ip", safeContext.ipAddress)
				.addKeyValue("client_user_agent", safeContext.userAgent)
				.addKeyValue("client_accept_language", safeContext.acceptLanguage)
				.addKeyValue("client_sec_ch_ua", safeContext.secChUa)
				.addKeyValue("client_sec_ch_ua_platform", safeContext.secChUaPlatform)
				.addKeyValue("client_sec_ch_ua_mobile", safeContext.secChUaMobile)
				.addKeyValue("client_origin", safeContext.origin)
				.log("Route planner client event")
		}

		return events.size
	}

	private companion object {
		const val MAX_HEADER_LENGTH = 512
		const val MAX_ORIGIN_LENGTH = 200
		const val MAX_IP_LENGTH = 64
	}

	private fun ClientRequestContext.sanitized() =
		copy(
			origin = ClientEventSanitizer.safeText(origin, MAX_ORIGIN_LENGTH),
			ipAddress =
				ClientEventSanitizer.safeText(ipAddress, MAX_IP_LENGTH)
					?.takeIf { IP_PATTERN.matches(it) },
			userAgent = ClientEventSanitizer.safeText(userAgent, MAX_HEADER_LENGTH),
			acceptLanguage = ClientEventSanitizer.safeText(acceptLanguage, MAX_HEADER_LENGTH),
			secChUa = ClientEventSanitizer.safeText(secChUa, MAX_HEADER_LENGTH),
			secChUaPlatform = ClientEventSanitizer.safeText(secChUaPlatform, MAX_HEADER_LENGTH),
			secChUaMobile = ClientEventSanitizer.safeText(secChUaMobile, MAX_HEADER_LENGTH),
		)

	private val IP_PATTERN = Regex("[0-9a-fA-F:.]{2,64}")
}

data class ClientRequestContext(
	val origin: String? = null,
	val ipAddress: String? = null,
	val userAgent: String? = null,
	val acceptLanguage: String? = null,
	val secChUa: String? = null,
	val secChUaPlatform: String? = null,
	val secChUaMobile: String? = null,
)

internal object ClientEventSanitizer {
	private const val MAX_BODY_LENGTH = 64 * 1024
	private const val MAX_BATCH_SIZE = 25
	private const val MAX_TEXT_LENGTH = 160
	private const val MAX_PAGE_LENGTH = 120
	private const val MAX_TYPE_LENGTH = 64
	private const val MAX_SMALL_FIELD_LENGTH = 40
	private const val MAX_VIEWPORT_SIZE = 20_000
	private const val MAX_SEQUENCE = 1_000_000
	private const val MAX_DURATION_MS = 86_400_000L
	private const val MAX_HARDWARE_CONCURRENCY = 1_024
	private val TYPE_PATTERN = Regex("[a-z][a-z0-9_.-]{0,63}")
	private val TOKEN_PATTERN = Regex("[a-zA-Z0-9_-]{1,160}")
	private val FIELD_STATE_PATTERN = Regex("(empty|nonempty|checked|unchecked|redacted)")

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
			clientId = safeToken(event.clientId),
			sessionId = safeToken(event.sessionId),
			pageViewId = safeToken(event.pageViewId),
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
			screenWidth = event.screenWidth?.takeIf { it in 0..MAX_VIEWPORT_SIZE },
			screenHeight = event.screenHeight?.takeIf { it in 0..MAX_VIEWPORT_SIZE },
			sequence = event.sequence?.takeIf { it in 0..MAX_SEQUENCE },
			elapsedMs = safeDuration(event.elapsedMs),
			sincePreviousMs = safeDuration(event.sincePreviousMs),
			durationMs = safeDuration(event.durationMs),
			changed = event.changed,
			fieldState = safeText(event.fieldState, MAX_SMALL_FIELD_LENGTH)?.takeIf(FIELD_STATE_PATTERN::matches),
			webdriver = event.webdriver,
			language = safeText(event.language, MAX_SMALL_FIELD_LENGTH),
			platform = safeText(event.platform, MAX_SMALL_FIELD_LENGTH),
			hardwareConcurrency =
				event.hardwareConcurrency?.takeIf { it in 0..MAX_HARDWARE_CONCURRENCY },
			maxTouchPoints = event.maxTouchPoints?.takeIf { it in 0..MAX_HARDWARE_CONCURRENCY },
		)
	}

	private fun safeToken(value: String?): String? =
		safeText(value)?.takeIf(TOKEN_PATTERN::matches)

	private fun parseInstant(value: String?): String? =
		runCatching { Instant.parse(value).toString() }.getOrNull()

	private fun safeDuration(value: Long?): Long? =
		value?.takeIf { it in 0..MAX_DURATION_MS }

	private val CONTROL_CHARACTERS = Regex("[\\u0000-\\u001f\\u007f]")
}

internal data class SanitizedClientEvent(
	val eventId: String?,
	val clientId: String?,
	val sessionId: String?,
	val pageViewId: String?,
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
	val screenWidth: Int?,
	val screenHeight: Int?,
	val sequence: Int?,
	val elapsedMs: Long?,
	val sincePreviousMs: Long?,
	val durationMs: Long?,
	val changed: Boolean?,
	val fieldState: String?,
	val webdriver: Boolean?,
	val language: String?,
	val platform: String?,
	val hardwareConcurrency: Int?,
	val maxTouchPoints: Int?,
)
