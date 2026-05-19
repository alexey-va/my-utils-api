package dev.myutils.api.temporal

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component

@Component
@ConditionalOnProperty(
	prefix = "myutils.temporal",
	name = ["enabled", "evening-reminder-enabled"],
	havingValue = "true",
)
@ConditionalOnTelegramBot
class TemporalReminderBootstrap(
	private val properties: MyUtilsProperties,
	private val temporalWorkflowService: TemporalWorkflowService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@EventListener(ApplicationReadyEvent::class)
	fun startEveningReminders() {
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
