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
			    "clientId": "client-1",
			    "sessionId": "session-1",
			    "pageViewId": "view-1",
			    "occurredAt": "2026-07-28T10:15:30Z",
			    "sequence": 7,
			    "elapsedMs": 1250,
			    "sincePreviousMs": 300,
			    "type": "click",
			    "page": "/route?address=secret",
			    "uiMode": "legacy",
			    "targetTag": "button",
			    "targetKey": "build-route",
			    "targetType": "button",
			    "detail": "line1\nline2",
			    "viewportWidth": 1440,
			    "viewportHeight": 900,
			    "screenWidth": 1920,
			    "screenHeight": 1080,
			    "durationMs": 820,
			    "changed": true,
			    "fieldState": "nonempty",
			    "webdriver": false,
			    "language": "ru-RU",
			    "platform": "macOS",
			    "hardwareConcurrency": 12,
			    "maxTouchPoints": 0,
			    "value": "password-that-must-not-be-logged"
			  }]
			}
			""".trimIndent()

		val batch = ClientEventSanitizer.parse(body, objectMapper)!!
		val event = batch.events.single()

		assertEquals("route-planner", batch.clientApp)
		assertEquals("click", event.type)
		assertEquals("/route", event.page)
		assertEquals("line1 line2", event.detail)
		assertEquals("build-route", event.targetKey)
		assertEquals(1440, event.viewportWidth)
		assertEquals("view-1", event.pageViewId)
		assertEquals("client-1", event.clientId)
		assertEquals(7, event.sequence)
		assertEquals(1250, event.elapsedMs)
		assertEquals(820, event.durationMs)
		assertEquals(true, event.changed)
		assertEquals("nonempty", event.fieldState)
		assertEquals("macOS", event.platform)
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
			      "viewportWidth": 999999,
			      "durationMs": -1,
			      "fieldState": "raw-secret",
			      "hardwareConcurrency": 999999
			    }
			  ]
			}
			""".trimIndent()

		val events = ClientEventSanitizer.parse(body, objectMapper)!!.events

		assertEquals(1, events.size)
		assertEquals("ui_error", events.single().type)
		assertNull(events.single().occurredAt)
		assertNull(events.single().viewportWidth)
		assertNull(events.single().durationMs)
		assertNull(events.single().fieldState)
		assertNull(events.single().hardwareConcurrency)
	}

	@Test
	fun `accepts my utils app and rejects unknown app`() {
		val myUtils =
			ClientEventSanitizer.parse(
				"""{"clientApp":"my-utils","events":[{"type":"page_view","page":"/"}]}""",
				objectMapper,
			)!!
		val unknown =
			ClientEventSanitizer.parse(
				"""{"clientApp":"some-other-site","events":[{"type":"page_view"}]}""",
				objectMapper,
			)

		assertEquals("my-utils", myUtils.clientApp)
		assertEquals("page_view", myUtils.events.single().type)
		assertNull(unknown)
	}
}
