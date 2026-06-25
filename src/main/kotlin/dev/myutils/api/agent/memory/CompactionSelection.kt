package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessage

internal object CompactionSelection {
	fun selectForCompaction(
		compactableOrdered: List<AgentConversationMessage>,
		tailKeep: Int,
		threshold: Int,
		force: Boolean,
	): List<AgentConversationMessage> {
		val count = compactableOrdered.size
		val toCompactCount =
			countForCompaction(
				compactableCount = count,
				tailKeep = tailKeep,
				threshold = threshold,
				force = force,
			)
		if (toCompactCount <= 0) {
			return emptyList()
		}
		return compactableOrdered.take(toCompactCount)
	}

	fun countForCompaction(
		compactableCount: Int,
		tailKeep: Int,
		threshold: Int,
		force: Boolean,
	): Int {
		if (compactableCount <= 1) {
			return 0
		}
		val effectiveTail = effectiveTailKeep(compactableCount, tailKeep, force)
		if (compactableCount <= effectiveTail) {
			return 0
		}
		val toCompactCount = compactableCount - effectiveTail
		if (!force && toCompactCount <= threshold) {
			return 0
		}
		return toCompactCount
	}

	private fun effectiveTailKeep(
		compactableCount: Int,
		tailKeep: Int,
		force: Boolean,
	): Int {
		if (!force) {
			return tailKeep
		}
		// Manual compact: keep at least one recent raw message, fewer than auto tail when history is short.
		return minOf(tailKeep, compactableCount - 1).coerceAtLeast(1)
	}
}
