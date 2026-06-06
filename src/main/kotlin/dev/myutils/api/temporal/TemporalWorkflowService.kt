package dev.myutils.api.temporal

import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.agent.AgentTurnInput
import dev.myutils.api.temporal.agent.WorkoutAgentWorkflow
import dev.myutils.api.temporal.notification.NotificationWorkflowInput
import dev.myutils.api.temporal.notification.TelegramNotificationWorkflow
import dev.myutils.api.temporal.reminder.EveningWorkoutReminderWorkflow
import dev.myutils.api.temporal.reminder.ReminderWorkflowInput
import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowExecutionAlreadyStarted
import io.temporal.client.WorkflowNotFoundException
import io.temporal.client.WorkflowOptions
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Service
import java.time.Instant
import java.time.ZonedDateTime
import java.util.UUID

@Service
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
class TemporalWorkflowService(
	private val workflowClient: WorkflowClient,
	private val properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun ensureEveningReminderRunning(chatId: Long) {
		if (!AppProperties.TEMPORAL_EVENING_REMINDER_ENABLED.get()) {
			log.info("Evening reminder disabled in runtime settings, skip chatId={}", chatId)
			return
		}
		val input =
			ReminderWorkflowInput(
				chatId = chatId,
				zoneId = AppProperties.TEMPORAL_ZONE_ID.get(),
				hour = AppProperties.TEMPORAL_EVENING_REMINDER_HOUR.get(),
				minute = AppProperties.TEMPORAL_EVENING_REMINDER_MINUTE.get(),
			)
		val workflowId = eveningReminderWorkflowId(chatId)
		val stub =
			workflowClient.newWorkflowStub(
				EveningWorkoutReminderWorkflow::class.java,
				workflowOptions(workflowId),
			)
		try {
			WorkflowClient.start(stub::run, input)
			log.info("Started evening reminder workflowId={} chatId={}", workflowId, chatId)
		} catch (_: WorkflowExecutionAlreadyStarted) {
			log.info("Evening reminder already running workflowId={}", workflowId)
		}
	}

	fun sendNotificationNow(
		chatId: Long,
		message: String,
	): String {
		val workflowId = notificationWorkflowId(chatId)
		startNotification(
			workflowId,
			NotificationWorkflowInput(
				chatId = chatId,
				message = message.trim(),
				deliverAtEpochMillis = Instant.now().toEpochMilli(),
			),
		)
		return workflowId
	}

	fun scheduleNotification(
		chatId: Long,
		message: String,
		deliverAt: ZonedDateTime,
	): String {
		val workflowId = notificationWorkflowId(chatId)
		startNotification(
			workflowId,
			NotificationWorkflowInput(
				chatId = chatId,
				message = message.trim(),
				deliverAtEpochMillis = deliverAt.toInstant().toEpochMilli(),
			),
		)
		return workflowId
	}

	fun cancelEveningReminder(chatId: Long) {
		val workflowId = eveningReminderWorkflowId(chatId)
		try {
			workflowClient.newUntypedWorkflowStub(workflowId).cancel()
			log.info("Cancelled evening reminder workflowId={}", workflowId)
		} catch (_: WorkflowNotFoundException) {
			log.debug("Evening reminder not found workflowId={}", workflowId)
		}
	}

	fun startAgentTurn(input: AgentTurnInput) {
		val workflowId = agentTurnWorkflowId(input.chatId)
		val stub =
			workflowClient.newWorkflowStub(
				WorkoutAgentWorkflow::class.java,
				workflowOptions(workflowId),
			)
		WorkflowClient.start(stub::handleTurn, input)
		log.info(
			"Started agent turn workflowId={} chatId={} userId={}",
			workflowId,
			input.chatId,
			input.userId,
		)
	}

	fun cancelNotification(workflowId: String): Boolean =
		try {
			workflowClient.newUntypedWorkflowStub(workflowId).cancel()
			log.info("Cancelled notification workflowId={}", workflowId)
			true
		} catch (_: WorkflowNotFoundException) {
			log.warn("Notification workflow not found workflowId={}", workflowId)
			false
		}

	private fun startNotification(
		workflowId: String,
		input: NotificationWorkflowInput,
	) {
		val stub =
			workflowClient.newWorkflowStub(
				TelegramNotificationWorkflow::class.java,
				workflowOptions(workflowId),
			)
		WorkflowClient.start(stub::deliver, input)
		log.info(
			"Started notification workflowId={} chatId={} deliverAt={}",
			workflowId,
			input.chatId,
			input.deliverAtEpochMillis,
		)
	}

	private fun workflowOptions(workflowId: String): WorkflowOptions =
		WorkflowOptions
			.newBuilder()
			.setWorkflowId(workflowId)
			.setTaskQueue(properties.temporal.taskQueue)
			.build()

	companion object {
		fun eveningReminderWorkflowId(chatId: Long): String = "evening-reminder-$chatId"

		fun notificationWorkflowId(chatId: Long): String = "tg-notify-$chatId-${UUID.randomUUID()}"

		fun agentTurnWorkflowId(chatId: Long): String = "agent-turn-$chatId-${UUID.randomUUID()}"
	}
}
