package dev.myutils.api.temporal

import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.temporal.reminder.EveningWorkoutReminderWorkflow
import dev.myutils.api.temporal.reminder.ReminderWorkflowInput
import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowExecutionAlreadyStarted
import io.temporal.client.WorkflowOptions
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Service

@Service
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
class TemporalWorkflowService(
	private val workflowClient: WorkflowClient,
	private val properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun ensureEveningReminderRunning(chatId: Long) {
		val temporal = properties.temporal
		val input =
			ReminderWorkflowInput(
				chatId = chatId,
				zoneId = temporal.zoneId,
				hour = temporal.eveningReminderHour,
				minute = temporal.eveningReminderMinute,
			)
		val workflowId = eveningReminderWorkflowId(chatId)
		val options =
			WorkflowOptions
				.newBuilder()
				.setWorkflowId(workflowId)
				.setTaskQueue(temporal.taskQueue)
				.build()
		val stub =
			workflowClient.newWorkflowStub(
				EveningWorkoutReminderWorkflow::class.java,
				options,
			)
		try {
			WorkflowClient.start(stub::run, input)
			log.info("Started Temporal evening reminder workflowId={} chatId={}", workflowId, chatId)
		} catch (_: WorkflowExecutionAlreadyStarted) {
			log.info("Temporal evening reminder already running workflowId={}", workflowId)
		}
	}

	companion object {
		fun eveningReminderWorkflowId(chatId: Long): String = "evening-reminder-$chatId"
	}
}
