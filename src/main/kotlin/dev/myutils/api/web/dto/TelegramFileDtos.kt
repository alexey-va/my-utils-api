package dev.myutils.api.web.dto

data class TelegramFileUploadResponse(
	val ok: Boolean,
	val fileName: String,
	val sizeBytes: Long,
	val sentTo: Int,
)
