package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentTestChat
import dev.myutils.api.domain.AgentTestChatRepository
import dev.myutils.api.infra.config.MyUtilsProperties
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.Instant
import java.util.UUID

@Service
class AgentTestChatService(
	private val repository: AgentTestChatRepository,
	private val messages: AgentConversationMessageRepository,
	private val memoryAdmin: AgentMemoryAdminService,
	private val chatTurnService: AgentChatTurnService,
	private val properties: MyUtilsProperties,
) {
	@Transactional
	fun create(title: String): AgentTestChatDto {
		val normalizedTitle = normalizeTitle(title)
		val row =
			repository.save(
				AgentTestChat(
					memoryChatId = repository.nextMemoryChatId(),
					userContextChatId = resolveUserContextChatId(),
					title = normalizedTitle,
				),
			)
		return row.toDto()
	}

	fun list(): List<AgentTestChatDto> =
		repository.findAllByOrderByUpdatedAtDesc().map { it.toDto() }

	fun get(id: UUID): AgentTestChatDto = requireChat(id).toDto()

	fun listMessages(
		id: UUID,
		beforeId: Long?,
		limit: Int,
	): AgentMemoryMessagePage {
		val row = requireChat(id)
		return memoryAdmin.listMessages(row.memoryChatId, beforeId, limit)
	}

	fun sendMessage(
		id: UUID,
		content: String,
		images: List<String>? = null,
	): AgentMemoryChatTurnResult {
		val row = requireChat(id)
		val result =
			chatTurnService.runSyncTurn(
				chatId = row.memoryChatId,
				text = content,
				images = images,
				contextChatId = row.userContextChatId,
			)
		touch(row)
		return result
	}

	@Transactional
	fun clearMessages(id: UUID) {
		val row = requireChat(id)
		memoryAdmin.clearDialog(row.memoryChatId)
		touch(row)
	}

	@Transactional
	fun rename(
		id: UUID,
		title: String,
	): AgentTestChatDto {
		val row = requireChat(id)
		row.title = normalizeTitle(title)
		row.updatedAt = Instant.now()
		return repository.save(row).toDto()
	}

	@Transactional
	fun delete(id: UUID) {
		val row = requireChat(id)
		memoryAdmin.clearDialog(row.memoryChatId)
		repository.delete(row)
	}

	internal fun requireChat(id: UUID): AgentTestChat =
		repository.findById(id).orElseThrow {
			ResponseStatusException(HttpStatus.NOT_FOUND, "Тестовый чат не найден.")
		}

	private fun touch(row: AgentTestChat) {
		row.updatedAt = Instant.now()
		repository.save(row)
	}

	private fun AgentTestChat.toDto(): AgentTestChatDto =
		AgentTestChatDto(
			id = id,
			memoryChatId = memoryChatId,
			userContextChatId = userContextChatId,
			title = title,
			messageCount = messages.countByChatId(memoryChatId),
			createdAt = createdAt,
			updatedAt = updatedAt,
		)

	private fun resolveUserContextChatId(): Long =
		properties.telegram.allowedUserIdSet().firstOrNull() ?: DEFAULT_USER_CONTEXT_CHAT_ID

	private fun normalizeTitle(title: String): String {
		val normalized = title.trim()
		require(normalized.isNotEmpty()) { "Название чата не может быть пустым." }
		require(normalized.length <= MAX_TITLE_LENGTH) {
			"Название чата слишком длинное (макс. $MAX_TITLE_LENGTH символов)."
		}
		return normalized
	}

	private companion object {
		const val DEFAULT_USER_CONTEXT_CHAT_ID = 1L
		const val MAX_TITLE_LENGTH = 120
	}
}

data class AgentTestChatDto(
	val id: UUID,
	val memoryChatId: Long,
	val userContextChatId: Long,
	val title: String,
	val messageCount: Long,
	val createdAt: Instant,
	val updatedAt: Instant,
)
