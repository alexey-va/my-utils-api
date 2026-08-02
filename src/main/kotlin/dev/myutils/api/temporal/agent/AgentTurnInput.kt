package dev.myutils.api.temporal.agent

data class AgentTurnInput(
	val chatId: Long,
	val userId: Long,
	val text: String,
	val maxToolIterations: Int = 8,
	val traceParent: String? = null,
	/** Исходный текст для разрешения мутаций, когда user message уже сохранён в memory (например с изображением). */
	val mutationAuthorizationText: String? = null,
	/** false — не отправлять ответ в Telegram (admin simulate). */
	val deliverToTelegram: Boolean = true,
)
