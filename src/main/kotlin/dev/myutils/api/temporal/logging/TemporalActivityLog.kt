package dev.myutils.api.temporal.logging

import io.temporal.activity.Activity
import org.slf4j.spi.LoggingEventBuilder

/** Поля Temporal для structured-логов в activities (привязка к workflow). */
object TemporalActivityLog {
	fun enrich(builder: LoggingEventBuilder): LoggingEventBuilder {
		val info = runCatching { Activity.getExecutionContext().info }.getOrNull() ?: return builder
		return builder
			.addKeyValue("workflowId", info.workflowId)
			.addKeyValue("runId", info.runId)
			.addKeyValue("workflowType", info.workflowType)
			.addKeyValue("activityType", info.activityType)
			.addKeyValue("activityAttempt", info.attempt)
	}
}
