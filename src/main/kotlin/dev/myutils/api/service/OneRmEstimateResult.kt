package dev.myutils.api.service

/** Результат estimate_1rm: картинка в Telegram + сводка для агента. */
data class OneRmEstimateResult(
	val png: ByteArray,
	val caption: String,
	val agentSummary: String,
)
