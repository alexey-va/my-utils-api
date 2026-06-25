package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessage

internal object CompactionSelection {
	fun selectForCompaction(
		compactableOrdered: List<AgentConversationMessage>,
		tailKeep: Int,
		threshold: Int,
		force: Boolean,
	): List<AgentConversationMessage> {
		if (compactableOrdered.size <= tailKeep) {
			return emptyList()
		}
		val toCompactCount = compactableOrdered.size - tailKeep
		if (!force && toCompactCount <= threshold) {
			return emptyList()
		}
		return compactableOrdered.take(toCompactCount)
	}
}
