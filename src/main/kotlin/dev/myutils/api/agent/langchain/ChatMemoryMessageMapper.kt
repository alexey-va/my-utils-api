package dev.myutils.api.agent.langchain

import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.TextContent
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.myutils.api.agent.memory.AgentMessageImages
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.infra.openrouter.ToolCall
import dev.myutils.api.infra.openrouter.ToolCallFunction

internal object ChatMemoryMessageMapper {
	fun toLangChain(dto: ChatMessage): LcChatMessage? =
		when (dto.role.lowercase()) {
			"user" -> AgentMessageImages.toUserMessage(dto.content, dto.images)
			"assistant" -> dto.toAiMessage()
			"tool" -> dto.toToolResultMessage()
			"system" -> dto.content?.let { SystemMessage.from(it) }
			else -> null
		}

	fun toDto(message: LcChatMessage): ChatMessage? =
		when (message) {
			is UserMessage ->
				ChatMessage(
					role = "user",
					content = userMessageText(message),
					images = AgentMessageImages.fromUserMessage(message).takeIf { it.isNotEmpty() },
				)
			is AiMessage -> message.toDto()
			is SystemMessage -> ChatMessage(role = "system", content = message.text())
			is ToolExecutionResultMessage ->
				ChatMessage(
					role = "tool",
					content = message.text(),
					toolCallId = message.id(),
					name = message.toolName(),
				)
			else -> null
		}

	private fun userMessageText(message: UserMessage): String? {
		if (message.hasSingleText()) {
			return message.singleText().trim().ifBlank { null }
		}
		return message
			.contents()
			.mapNotNull { part ->
				if (part is TextContent) {
					part.text().trim()
				} else {
					null
				}
			}.joinToString("\n")
			.trim()
			.ifBlank { null }
	}

	private fun ChatMessage.toAiMessage(): AiMessage? {
		val requests =
			toolCalls?.map { tc ->
				ToolExecutionRequest
					.builder()
					.id(tc.id)
					.name(tc.function.name)
					.arguments(tc.function.arguments)
					.build()
			} ?: emptyList()
		return when {
			requests.isNotEmpty() && !content.isNullOrBlank() -> AiMessage.from(content, requests)
			requests.isNotEmpty() -> AiMessage.from(requests)
			!content.isNullOrBlank() -> AiMessage.from(content!!)
			else -> null
		}
	}

	private fun ChatMessage.toToolResultMessage(): ToolExecutionResultMessage? {
		val id = toolCallId ?: return null
		val toolName = name ?: return null
		return ToolExecutionResultMessage.from(id, toolName, content.orEmpty())
	}

	private fun AiMessage.toDto(): ChatMessage {
		val requests = toolExecutionRequests()
		return ChatMessage(
			role = "assistant",
			content = text()?.takeIf { it.isNotBlank() },
			toolCalls =
				requests.takeIf { it.isNotEmpty() }?.map { req ->
					ToolCall(
						id = req.id(),
						function =
							ToolCallFunction(
								name = req.name(),
								arguments = req.arguments(),
							),
					)
				},
		)
	}
}
