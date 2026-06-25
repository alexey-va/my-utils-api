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
				val conf = formatConfidence(fact.confidence)
				val added = formatFactDate(fact.createdAt)
				appendLine("• [${fact.id}] ($conf, $added) ${fact.content.trim()}")
			}
		}.trimEnd()
	}

	@Transactional
	fun remember(
		chatId: Long,
		content: String,
		confidence: Double? = null,
	): String {
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		require(trimmed.length <= MAX_FACT_LENGTH) { "Факт слишком длинный (макс. $MAX_FACT_LENGTH символов)." }
		val fact =
			repository.save(
				AgentUserFact(
					chatId = chatId,
					content = trimmed,
					confidence = FactConfidence.normalize(confidence),
				),
			)
		return "Запомнил факт [${fact.id}] (${formatConfidence(fact.confidence)}): ${fact.content}"
	}

	@Transactional
	fun update(
		chatId: Long,
		factId: String,
		content: String,
		confidence: Double? = null,
	): String {
		val id = parseFactId(factId)
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		require(trimmed.length <= MAX_FACT_LENGTH) { "Факт слишком длинный (макс. $MAX_FACT_LENGTH символов)." }
		val fact =
			repository.findByIdAndChatId(id, chatId).orElse(null)
				?: return "Факт $factId не найден для этого чата."
		fact.content = trimmed
		if (confidence != null) {
			fact.confidence = FactConfidence.normalize(confidence)
		}
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

		private fun formatConfidence(value: Double): String =
			"conf ${String.format("%.2f", value.coerceIn(0.0, 1.0))}"

		private fun formatFactDate(instant: Instant): String =
			instant.atZone(java.time.ZoneId.systemDefault()).toLocalDate().toString()
	}
}
