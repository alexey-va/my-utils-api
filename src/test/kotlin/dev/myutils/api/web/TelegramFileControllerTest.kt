package dev.myutils.api.web

import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.InMemoryTelegramMessenger
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.mock.web.MockMultipartFile
import org.springframework.test.context.TestPropertySource
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.multipart

@AutoConfigureMockMvc
@TestPropertySource(
	properties = [
		"myutils.telegram.allowed-user-ids=999",
		"myutils.telegram.file-upload-token=test-file-token",
	],
)
class TelegramFileControllerTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var telegram: InMemoryTelegramMessenger

	@BeforeEach
	fun clearTelegram() {
		telegram.clear()
	}

	@Test
	fun `delivers multipart file to configured Telegram user`() {
		val bytes = "shortcut-content".toByteArray()
		val file = MockMultipartFile("file", "../Health Sync.shortcut", "application/octet-stream", bytes)

		val response =
			mockMvc
				.multipart("/api/telegram/files") {
					file(file)
					param("caption", "Готовый шорткат")
					header("X-Telegram-File-Token", "test-file-token")
				}.andReturn()
				.response

		assertEquals(200, response.status)
		assertTrue(response.contentAsString.contains("\"ok\":true"))
		assertTrue(response.contentAsString.contains("\"fileName\":\"Health Sync.shortcut\""))
		assertTrue(response.contentAsString.contains("\"sentTo\":1"))

		val sent = telegram.documentsFor(999).single()
		assertArrayEquals(bytes, sent.bytes)
		assertEquals("Health Sync.shortcut", sent.fileName)
		assertEquals("application/octet-stream", sent.contentType)
		assertEquals("Готовый шорткат", sent.caption)
	}

	@Test
	fun `rejects invalid file token`() {
		val file = MockMultipartFile("file", "note.txt", "text/plain", "hello".toByteArray())

		val response =
			mockMvc
				.multipart("/api/telegram/files") {
					file(file)
					header("X-Telegram-File-Token", "wrong")
				}.andReturn()
				.response

		assertEquals(401, response.status)
		assertTrue(telegram.documentsFor(999).isEmpty())
	}
}
