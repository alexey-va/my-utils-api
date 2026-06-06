package dev.myutils.api.agent

import dev.myutils.api.service.WorkoutBotFacade
import org.slf4j.LoggerFactory
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.springframework.stereotype.Component

/** Свежий снимок дневника для промпта (не сохраняется в Redis-историю). */
@Component
@ConditionalOnTelegramBot
class WorkoutAgentContextBuilder(
	private val workoutBotFacade: WorkoutBotFacade,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun buildSnapshot(): String {
		val snapshot = workoutBotFacade.buildAgentSnapshot()
		log.info("Agent context snapshot built, {} chars", snapshot.length)
		return snapshot
	}
}
