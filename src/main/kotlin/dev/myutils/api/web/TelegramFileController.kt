package dev.myutils.api.web

import dev.myutils.api.service.TelegramFileDeliveryService
import dev.myutils.api.web.dto.TelegramFileUploadResponse
import org.springframework.http.MediaType
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import org.springframework.web.multipart.MultipartFile

@RestController
@RequestMapping("/api/telegram")
class TelegramFileController(
	private val telegramFileDeliveryService: TelegramFileDeliveryService,
) {
	@PostMapping("/files", consumes = [MediaType.MULTIPART_FORM_DATA_VALUE])
	fun upload(
		@RequestHeader("X-Telegram-File-Token", required = false) token: String?,
		@RequestParam file: MultipartFile,
		@RequestParam(required = false) caption: String?,
	): TelegramFileUploadResponse = telegramFileDeliveryService.deliver(token, file, caption)
}
