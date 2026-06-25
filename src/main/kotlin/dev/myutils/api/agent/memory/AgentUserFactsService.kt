package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentUserFact
import dev.myutils.api.domain.AgentUserFactRepository
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import java.time.Instant
import java.util.UUID

@Service
@ConditionalOnTelegramBot
class AgentUserFactsService(
	private val repository: AgentUserFactRepository,
) {
	fun list(chatId: Long): List<AgentUserFact> = repository.findByChatIdOrderByUpdatedAtDesc(chatId)

	fun formatForPrompt(chatId: Long): String {
		val facts = list(chatId)
		if (facts.isEmpty()) {
			return "Известные факты о пользователе: (пока нет)"
		}
		return buildString {
			appendLine("Известные факты о пользователе (память агента):")
			facts.forEach { fact ->
				appendLine("• [${fact.id}] ${fact.content.trim()}")
			}
		}.trimEnd()
	}

	@Transactional
	fun remember(
		chatId: Long,
		content: String,
	): String {
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		require(trimmed.length <= MAX_FACT_LENGTH) { "Факт слишком длинный (макс. $MAX_FACT_LENGTH символов)." }
		val fact = repository.save(AgentUserFact(chatId = chatId, content = trimmed))
		return "Запомнил факт [${fact.id}]: ${fact.content}"
	}

	@Transactional
	fun update(
		chatId: Long,
		factId: String,
		content: String,
	): String {
		val id = parseFactId(factId)
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		require(trimmed.length <= MAX_FACT_LENGTH) { "Факт слишком длинный (макс. $MAX_FACT_LENGTH символов)." }
		val fact =
			repository.findByIdAndChatId(id, chatId).orElse(null)
				?: return "Факт $factId не найден для этого чата."
		fact.content = trimmed
		fact.updatedAt = Instant.now()
		repository.save(fact)
		return "Обновил факт [${fact.id}]: ${fact.content}"
	}

	@Transactional
	fun forget(
		chatId: Long,
		factId: String,
	): String {
		val id = parseFactId(factId)
		val fact = repository.findByIdAndChatId(id, chatId).orElse(null)
		if (fact == null) {
			return "Факт $factId не найден для этого чата."
		}
		repository.delete(fact)
		return "Удалил факт [${fact.id}]."
	}

	private fun parseFactId(raw: String): UUID =
		runCatching { UUID.fromString(raw.trim()) }
			.getOrElse { throw IllegalArgumentException("Некорректный fact_id: $raw") }

	private companion object {
		const val MAX_FACT_LENGTH = 2_000
	}
}
