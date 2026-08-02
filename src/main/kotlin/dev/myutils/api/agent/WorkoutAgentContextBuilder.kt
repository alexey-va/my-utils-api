package dev.myutils.api.agent

import dev.myutils.api.agent.memory.AgentTestSandboxService
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.service.WorkoutBotFacade
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

/** Свежий снимок дневника для промпта (не сохраняется в историю диалога). */
@Component
@ConditionalOnTelegramBot
class WorkoutAgentContextBuilder(
	private val workoutBotFacade: WorkoutBotFacade,
	private val sandbox: AgentTestSandboxService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun buildSnapshot(contextChatId: Long? = null): String {
		val snapshot =
			if (contextChatId != null && sandbox.isSandboxChatId(contextChatId)) {
				sandbox.buildSnapshot(contextChatId)
			} else {
				workoutBotFacade.buildAgentSnapshot()
			}
		log.info("Agent context snapshot built, {} chars", snapshot.length)
		return snapshot
	}
}
