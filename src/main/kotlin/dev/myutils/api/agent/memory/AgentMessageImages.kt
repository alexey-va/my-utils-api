package dev.myutils.api.agent.memory

import dev.langchain4j.data.message.Content
import dev.langchain4j.data.message.ImageContent
import dev.langchain4j.data.message.TextContent
import dev.langchain4j.data.message.UserMessage
import dev.myutils.api.infra.openrouter.ChatMessage

internal object AgentMessageImages {
	const val MAX_IMAGES = 4
	const val MAX_DATA_URL_CHARS = 5_500_000

	fun normalize(images: List<String>?): List<String> {
		if (images.isNullOrEmpty()) {
			return emptyList()
		}
		require(images.size <= MAX_IMAGES) { "Не больше $MAX_IMAGES изображений за раз." }
		return images.map { normalizeDataUrl(it) }
	}

	fun hasPayload(
		content: String?,
		images: List<String>?,
	): Boolean = !content.isNullOrBlank() || !images.isNullOrEmpty()

	fun toUserMessage(
		content: String?,
		images: List<String>?,
	): UserMessage? {
		val normalizedImages = normalize(images)
		val text = content?.trim().orEmpty()
		if (text.isEmpty() && normalizedImages.isEmpty()) {
			return null
		}
		val parts = mutableListOf<Content>()
		if (text.isNotEmpty()) {
			parts.add(TextContent.from(text))
		}
		for (image in normalizedImages) {
			parts.add(ImageContent.from(image))
		}
		return UserMessage.from(parts)
	}

	fun fromUserMessage(message: UserMessage): List<String> =
		message.contents().mapNotNull { part ->
			if (part !is ImageContent) {
				return@mapNotNull null
			}
			val image = part.image()
			val url = image.url()?.toString()
			if (!url.isNullOrBlank()) {
				return@mapNotNull url
			}
			val base64 = image.base64Data()
			val mime = image.mimeType()
			if (!base64.isNullOrBlank() && !mime.isNullOrBlank()) {
				"data:$mime;base64,$base64"
			} else {
				null
			}
		}

	private fun normalizeDataUrl(raw: String): String {
		val trimmed = raw.trim()
		require(trimmed.startsWith("data:image/")) {
			"Изображение должно быть data URL (data:image/…;base64,…)."
		}
		require(trimmed.length <= MAX_DATA_URL_CHARS) {
			"Изображение слишком большое."
		}
		return trimmed
	}
}
