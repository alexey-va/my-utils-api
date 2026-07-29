package dev.myutils.api.web

import ch.qos.logback.classic.Logger
import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.read.ListAppender
import dev.myutils.api.service.ClientEventLoggingService
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.post

@AutoConfigureMockMvc
class ClientEventControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	private lateinit var logger: Logger
	private lateinit var appender: ListAppender<ILoggingEvent>

	@BeforeEach
	fun captureClientEventLogs() {
		logger = LoggerFactory.getLogger(ClientEventLoggingService::class.java) as Logger
		appender = ListAppender<ILoggingEvent>().also { it.start() }
		logger.addAppender(appender)
	}

	@AfterEach
	fun stopCapturingClientEventLogs() {
		logger.detachAppender(appender)
		appender.stop()
	}

	@Test
	fun `accepts anonymous route event and writes structured safe log`() {
		val body =
			"""
			{
			  "events": [{
			    "eventId": "event-1",
			    "clientId": "client-1",
			    "sessionId": "session-1",
			    "pageViewId": "view-1",
			    "occurredAt": "2026-07-28T10:15:30Z",
			    "sequence": 3,
			    "elapsedMs": 1200,
			    "durationMs": 700,
			    "changed": true,
			    "fieldState": "nonempty",
			    "webdriver": true,
			    "platform": "macOS",
			    "type": "click",
			    "page": "/",
			    "uiMode": "legacy",
			    "targetTag": "button",
			    "targetKey": "build-route",
			    "value": "super-secret-input"
			  }]
			}
			""".trimIndent()

		mockMvc
			.post("/api/client-events") {
				contentType = MediaType.TEXT_PLAIN
				header("Origin", "https://route.alexeyav.ru")
				header("X-Real-IP", "203.0.113.42")
				header("X-Forwarded-For", "198.51.100.99")
				header("User-Agent", "agent-browser/1.2 Playwright")
				header("Accept-Language", "ru-RU,ru;q=0.9")
				header("Sec-CH-UA", "\"Chromium\";v=\"138\"")
				header("Sec-CH-UA-Platform", "\"macOS\"")
				header("Sec-CH-UA-Mobile", "?0")
				content = body
			}.andExpect {
				status { isNoContent() }
				header { string("Access-Control-Allow-Origin", "https://route.alexeyav.ru") }
			}

		val logEvent = appender.list.single()
		val fields = logEvent.keyValuePairs.associate { it.key to it.value }
		assertTrue(logEvent.formattedMessage.contains("Client event"))
		assertTrue(fields["event_type"] == "client_event")
		assertTrue(fields["client_app"] == "route-planner")
		assertTrue(fields["client_event_type"] == "click")
		assertTrue(fields["client_id"] == "client-1")
		assertTrue(fields["client_target_key"] == "build-route")
		assertTrue(fields["client_page_view_id"] == "view-1")
		assertTrue(fields["client_sequence"] == 3)
		assertTrue(fields["client_duration_ms"] == 700L)
		assertTrue(fields["client_changed"] == true)
		assertTrue(fields["client_field_state"] == "nonempty")
		assertTrue(fields["client_webdriver"] == true)
		assertTrue(fields["client_ip"] == "203.0.113.42")
		assertTrue(fields["client_user_agent"] == "agent-browser/1.2 Playwright")
		assertFalse(fields.values.any { it?.toString()?.contains("198.51.100.99") == true })
		assertFalse(fields.values.any { it?.toString()?.contains("super-secret-input") == true })
	}

	@Test
	fun `accepts anonymous Workout visit with server side visitor markers`() {
		val body =
			"""
			{
			  "clientApp": "my-utils",
			  "events": [{
			    "eventId": "visit-1",
			    "clientId": "workout-client-1",
			    "sessionId": "workout-session-1",
			    "pageViewId": "workout-view-1",
			    "occurredAt": "2026-07-29T12:00:00Z",
			    "type": "page_view",
			    "page": "/",
			    "uiMode": "workout",
			    "platform": "Windows"
			  }]
			}
			""".trimIndent()

		mockMvc
			.post("/api/client-events") {
				contentType = MediaType.TEXT_PLAIN
				header("Origin", "https://utils.alexeyav.ru")
				header("X-Real-IP", "203.0.113.73")
				header("User-Agent", "Mozilla/5.0 Workout Browser")
				content = body
			}.andExpect {
				status { isNoContent() }
			}

		val fields = appender.list.single().keyValuePairs.associate { it.key to it.value }
		assertTrue(fields["client_app"] == "my-utils")
		assertTrue(fields["client_event_type"] == "page_view")
		assertTrue(fields["client_id"] == "workout-client-1")
		assertTrue(fields["client_ip"] == "203.0.113.73")
		assertTrue(fields["client_user_agent"] == "Mozilla/5.0 Workout Browser")
	}

	@Test
	fun `invalid payload is ignored without failing the client`() {
		mockMvc
			.post("/api/client-events") {
				contentType = MediaType.TEXT_PLAIN
				content = "not-json"
			}.andExpect {
				status { isNoContent() }
			}

		assertTrue(appender.list.isEmpty())
	}
}
