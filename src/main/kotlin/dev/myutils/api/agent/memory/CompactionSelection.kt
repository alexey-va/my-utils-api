package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessage

internal object CompactionSelection {
	fun <T> rewindSplitToolTurn(
		ordered: List<T>,
		selectedCount: Int,
		roleOf: (T) -> String?,
	): Int {
		if (selectedCount <= 0 || selectedCount >= ordered.size) {
			return selectedCount.coerceIn(0, ordered.size)
		}
		if (!roleOf(ordered[selectedCount]).equals("tool", ignoreCase = true)) {
			return selectedCount
		}

		var boundary = selectedCount
		while (boundary > 0 && roleOf(ordered[boundary - 1]).equals("tool", ignoreCase = true)) {
			boundary--
		}
		if (boundary > 0 && roleOf(ordered[boundary - 1]).equals("assistant", ignoreCase = true)) {
			boundary--
		}
		return boundary
	}

	fun selectForAutoCompaction(
		compactableOrdered: List<AgentConversationMessage>,
		tailKeep: Int,
		threshold: Int,
	): List<AgentConversationMessage> {
		val toCompactCount =
			countForAutoCompaction(
				compactableCount = compactableOrdered.size,
				tailKeep = tailKeep,
				threshold = threshold,
			)
		if (toCompactCount <= 0) {
			return emptyList()
		}
		return compactableOrdered.take(toCompactCount)
	}

	fun countForAutoCompaction(
		compactableCount: Int,
		tailKeep: Int,
		threshold: Int,
	): Int {
		if (compactableCount <= tailKeep) {
			return 0
		}
		val toCompactCount = compactableCount - tailKeep
		if (toCompactCount <= threshold) {
			return 0
		}
		return toCompactCount
	}

	/** Admin manual compact: no threshold; keepRecent=0 compresses entire history. */
	fun selectForAdminCompaction(
		compactableOrdered: List<AgentConversationMessage>,
		keepRecent: Int,
	): List<AgentConversationMessage> {
		val toCompactCount =
			countForAdminCompaction(
				compactableCount = compactableOrdered.size,
				keepRecent = keepRecent,
			)
		if (toCompactCount <= 0) {
			return emptyList()
		}
		return compactableOrdered.take(toCompactCount)
	}

	fun countForAdminCompaction(
		compactableCount: Int,
		keepRecent: Int,
	): Int {
		val keep = keepRecent.coerceAtLeast(0)
		if (compactableCount <= keep) {
			return 0
		}
		return compactableCount - keep
	}
}
