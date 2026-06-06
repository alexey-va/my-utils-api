package dev.myutils.api.temporal.agent

data class AgentTurnMetricsInput(
	val outcome: String,
	val durationMs: Long,
	val llmSteps: Int,
)
