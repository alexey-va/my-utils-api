package dev.myutils.api.service

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class ClientEventSanitizerTest {
	private val objectMapper = jacksonObjectMapper()

	@Test
	fun `keeps only fixed sanitized event fields`() {
		val body =
			"""
			{
			  "events": [{
			    "eventId": "event-1",
			    "sessionId": "session-1",
			    "occurredAt": "2026-07-28T10:15:30Z",
			    "type": "click",
			    "page": "/route?address=secret",
			    "uiMode": "legacy",
			    "targetTag": "button",
			    "targetKey": "build-route",
			    "targetType": "button",
			    "detail": "line1\nline2",
			    "viewportWidth": 1440,
			    "viewportHeight": 900,
			    "value": "password-that-must-not-be-logged"
			  }]
			}
			""".trimIndent()

		val event = ClientEventSanitizer.parse(body, objectMapper).single()

		assertEquals("click", event.type)
		assertEquals("/route", event.page)
		assertEquals("line1 line2", event.detail)
		assertEquals("build-route", event.targetKey)
		assertEquals(1440, event.viewportWidth)
	}

	@Test
	fun `drops malformed event types and invalid optional values`() {
		val body =
			"""
			{
			  "events": [
			    {"type": "contains spaces"},
			    {
			      "type": "ui_error",
			      "occurredAt": "not-an-instant",
			      "viewportWidth": 999999
			    }
			  ]
			}
			""".trimIndent()

		val events = ClientEventSanitizer.parse(body, objectMapper)

		assertEquals(1, events.size)
		assertEquals("ui_error", events.single().type)
		assertNull(events.single().occurredAt)
		assertNull(events.single().viewportWidth)
	}
}
