package dev.myutils.api.agent.memory

internal object FactConfidence {
	const val AGENT_DEFAULT = 0.85
	const val ADMIN_DEFAULT = 1.0

	fun normalize(raw: Double?): Double {
		if (raw == null) return AGENT_DEFAULT
		return raw.coerceIn(0.0, 1.0)
	}

	fun normalizeOrDefault(
		raw: Double?,
		default: Double,
	): Double {
		if (raw == null) return default.coerceIn(0.0, 1.0)
		return raw.coerceIn(0.0, 1.0)
	}
}
