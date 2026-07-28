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
			    "sessionId": "session-1",
			    "occurredAt": "2026-07-28T10:15:30Z",
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
				content = body
			}.andExpect {
				status { isNoContent() }
				header { string("Access-Control-Allow-Origin", "https://route.alexeyav.ru") }
			}

		val logEvent = appender.list.single()
		val fields = logEvent.keyValuePairs.associate { it.key to it.value }
		assertTrue(logEvent.formattedMessage.contains("Route planner client event"))
		assertTrue(fields["event_type"] == "client_event")
		assertTrue(fields["client_app"] == "route-planner")
		assertTrue(fields["client_event_type"] == "click")
		assertTrue(fields["client_target_key"] == "build-route")
		assertFalse(fields.values.any { it?.toString()?.contains("super-secret-input") == true })
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
