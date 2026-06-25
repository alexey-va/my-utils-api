package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.infra.openrouter.ChatMessage

internal object StoredMessageFilter {
	fun isSystemRole(role: String?): Boolean = role.equals("system", ignoreCase = true)

	fun isDialogRole(role: String?): Boolean = !isSystemRole(role)

	fun roleFromJson(
		messageJson: String,
		objectMapper: ObjectMapper,
	): String? =
		runCatching { objectMapper.readValue<ChatMessage>(messageJson).role }.getOrNull()

	fun isSystemStored(
		row: AgentConversationMessage,
		objectMapper: ObjectMapper,
	): Boolean = isSystemRole(roleFromJson(row.messageJson, objectMapper))
}
