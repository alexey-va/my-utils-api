package dev.myutils.api.temporal.agent

data class AgentTurnInput(
	val chatId: Long,
	val userId: Long,
	val text: String,
	val maxToolIterations: Int = 8,
	val traceParent: String? = null,
)
