package dev.myutils.api.temporal.agent

data class AgentTurnInput(
	val chatId: Long,
	val userId: Long,
	val text: String,
)
