package dev.myutils.api.agent.langchain

import dev.myutils.api.telegram.TelegramChatHistory
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.store.memory.chat.ChatMemoryStore
import org.springframework.stereotype.Component

@Component
class RedisChatMemoryStore(
	private val telegramChatHistory: TelegramChatHistory,
) : ChatMemoryStore {
	override fun getMessages(memoryId: Any): List<LcChatMessage> =
		telegramChatHistory
			.load(memoryId as Long)
			.mapNotNull { ChatMemoryMessageMapper.toLangChain(it) }

	override fun updateMessages(
		memoryId: Any,
		messages: List<LcChatMessage>,
	) {
		telegramChatHistory.save(
			memoryId as Long,
			messages.mapNotNull { ChatMemoryMessageMapper.toDto(it) },
		)
	}

	override fun deleteMessages(memoryId: Any) {
		telegramChatHistory.save(memoryId as Long, emptyList())
	}
}
