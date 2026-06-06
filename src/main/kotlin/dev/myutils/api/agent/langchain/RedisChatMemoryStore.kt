package dev.myutils.api.agent.langchain

import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.telegram.TelegramChatHistory
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.store.memory.chat.ChatMemoryStore
import org.springframework.stereotype.Component

@Component
class RedisChatMemoryStore(
	private val telegramChatHistory: TelegramChatHistory,
) : ChatMemoryStore {
	override fun getMessages(memoryId: Any): List<LcChatMessage> =
		telegramChatHistory
			.load(memoryId as Long)
			.mapNotNull { it.toLangChain() }

	override fun updateMessages(
		memoryId: Any,
		messages: List<LcChatMessage>,
	) {
		telegramChatHistory.save(
			memoryId as Long,
			messages.mapNotNull { it.toDto() },
		)
	}

	override fun deleteMessages(memoryId: Any) {
		telegramChatHistory.save(memoryId as Long, emptyList())
	}
}

private fun ChatMessage.toLangChain(): LcChatMessage? =
	when (role.lowercase()) {
		"user" -> UserMessage.from(content)
		"assistant" -> AiMessage.from(content)
		"system" -> SystemMessage.from(content)
		else -> null
	}

private fun LcChatMessage.toDto(): ChatMessage? =
	when (this) {
		is UserMessage -> ChatMessage(role = "user", content = messageText())
		is AiMessage -> ChatMessage(role = "assistant", content = messageText())
		is SystemMessage -> ChatMessage(role = "system", content = messageText())
		is ToolExecutionResultMessage -> null
		else -> null
	}

private fun LcChatMessage.messageText(): String =
	when (this) {
		is UserMessage -> singleText() ?: ""
		is AiMessage -> text() ?: ""
		is SystemMessage -> text() ?: ""
		else -> ""
	}
