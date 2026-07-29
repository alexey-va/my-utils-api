package dev.myutils.api.service

import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.web.dto.TelegramFileUploadResponse
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.web.multipart.MultipartFile
import org.springframework.web.server.ResponseStatusException
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.HexFormat

@Service
class TelegramFileDeliveryService(
	private val properties: MyUtilsProperties,
	private val telegramProvider: ObjectProvider<TelegramMessenger>,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun deliver(
		providedToken: String?,
		file: MultipartFile,
		caption: String?,
	): TelegramFileUploadResponse {
		verifyToken(providedToken)
		if (file.isEmpty) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "File is empty")
		}
		if (file.size > MAX_FILE_SIZE_BYTES) {
			throw ResponseStatusException(HttpStatus.PAYLOAD_TOO_LARGE, "File is larger than 20 MB")
		}

		val chatIds = properties.telegram.allowedUserIdSet()
		if (chatIds.isEmpty()) {
			throw ResponseStatusException(
				HttpStatus.SERVICE_UNAVAILABLE,
				"TELEGRAM_ALLOWED_USER_IDS is not configured",
			)
		}
		val telegram =
			telegramProvider.getIfAvailable()
				?: throw ResponseStatusException(
					HttpStatus.SERVICE_UNAVAILABLE,
					"Telegram bot is not configured",
				)

		val fileName = safeFileName(file.originalFilename)
		val bytes = file.bytes
		val sentTo =
			chatIds.count { chatId ->
				runCatching {
					telegram.sendDocument(
						chatId = chatId,
						bytes = bytes,
						fileName = fileName,
						contentType = file.contentType,
						caption = caption?.trim()?.takeIf { it.isNotEmpty() },
					)
				}.onFailure { error ->
					log.warn("Telegram file delivery failed chatId={} fileName={}", chatId, fileName, error)
				}.getOrDefault(false)
			}

		if (sentTo == 0) {
			throw ResponseStatusException(HttpStatus.BAD_GATEWAY, "Telegram did not accept the file")
		}

		log.info(
			"Telegram file delivered fileName={} sizeBytes={} recipients={}",
			fileName,
			file.size,
			sentTo,
		)
		return TelegramFileUploadResponse(
			ok = true,
			fileName = fileName,
			sizeBytes = file.size,
			sentTo = sentTo,
		)
	}

	private fun verifyToken(providedToken: String?) {
		val expected = expectedToken()
		if (expected == null) {
			throw ResponseStatusException(
				HttpStatus.SERVICE_UNAVAILABLE,
				"Telegram file upload authentication is not configured",
			)
		}
		val matches =
			providedToken != null &&
				MessageDigest.isEqual(
					expected.toByteArray(StandardCharsets.UTF_8),
					providedToken.toByteArray(StandardCharsets.UTF_8),
				)
		if (!matches) {
			throw ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid Telegram file token")
		}
	}

	private fun expectedToken(): String? {
		properties.telegram.fileUploadToken.trim().takeIf { it.isNotEmpty() }?.let { return it }
		val botToken = properties.telegram.botToken.trim().takeIf { it.isNotEmpty() } ?: return null
		val digest =
			MessageDigest
				.getInstance("SHA-256")
				.digest("$TOKEN_DERIVATION_PREFIX$botToken".toByteArray(StandardCharsets.UTF_8))
		return HexFormat.of().formatHex(digest)
	}

	private fun safeFileName(original: String?): String {
		val normalized =
			original
				.orEmpty()
				.replace('\\', '/')
				.substringAfterLast('/')
				.trim()
				.take(MAX_FILE_NAME_LENGTH)
		return normalized.ifEmpty { "file.bin" }
	}

	private companion object {
		const val MAX_FILE_SIZE_BYTES = 20L * 1024 * 1024
		const val MAX_FILE_NAME_LENGTH = 255
		const val TOKEN_DERIVATION_PREFIX = "my-utils-file-upload:"
	}
}
