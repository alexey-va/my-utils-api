package dev.myutils.api.telegram

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

class TelegramButtonParserTest {
	@Test
	fun `empty buttons returns null`() {
		assertNull(TelegramButtonParser.parse(null))
		assertNull(TelegramButtonParser.parse(""))
		assertNull(TelegramButtonParser.parse("   "))
	}

	@Test
	fun `parses rows and buttons`() {
		val rows = TelegramButtonParser.parseRows("Сегодня:что на сегодня,Неделя:план;Отмена:отмена")!!
		assertEquals(2, rows.size)
		assertEquals(listOf("Сегодня" to "что на сегодня", "Неделя" to "план"), rows[0])
		assertEquals(listOf("Отмена" to "отмена"), rows[1])
	}

	@Test
	fun `rejects invalid format`() {
		assertThrows(IllegalArgumentException::class.java) {
			TelegramButtonParser.parse("без двоеточия")
		}
	}
}
