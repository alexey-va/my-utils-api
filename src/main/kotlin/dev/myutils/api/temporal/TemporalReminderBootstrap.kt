package dev.myutils.api.temporal

import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ConditionalOnTelegramBot
class TemporalReminderBootstrap(
	private val properties: MyUtilsProperties,
	private val temporalWorkflowService: TemporalWorkflowService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@EventListener(ApplicationReadyEvent::class)
	fun startEveningReminders() {
		if (!AppProperties.TEMPORAL_EVENING_REMINDER_ENABLED.get()) {
			log.info("Evening reminder disabled in runtime settings")
			return
		}
		val allowed = properties.telegram.allowedUserIdSet()
		if (allowed.isEmpty()) {
			log.warn("Temporal evening reminders not started: TELEGRAM_ALLOWED_USER_IDS is empty")
			return
		}
		for (userId in allowed) {
			temporalWorkflowService.ensureEveningReminderRunning(userId)
		}
		log.info("Temporal evening reminders ensured for {} user(s)", allowed.size)
	}
}
