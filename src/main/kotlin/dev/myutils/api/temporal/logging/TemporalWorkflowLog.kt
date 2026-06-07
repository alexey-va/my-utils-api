package dev.myutils.api.temporal.logging

import io.temporal.workflow.Workflow
import org.slf4j.Logger

/**
 * Replay-safe логирование внутри workflow ([Workflow.getLogger]).
 * Вызывать только из workflow-кода, не из activities.
 */
class TemporalWorkflowLog private constructor(
	private val log: Logger,
) {
	fun info(
		message: String,
		fields: Map<String, Any?> = emptyMap(),
	) {
		log.info(appendContext(message, fields))
	}

	fun warn(
		message: String,
		fields: Map<String, Any?> = emptyMap(),
	) {
		log.warn(appendContext(message, fields))
	}

	private fun appendContext(
		message: String,
		fields: Map<String, Any?>,
	): String {
		val info = Workflow.getInfo()
		val parts = buildList {
			add("workflowId=${info.workflowId}")
			add("runId=${info.runId}")
			add("workflowType=${info.workflowType}")
			fields.forEach { (key, value) ->
				if (value != null) {
					add("$key=$value")
				}
			}
		}
		return "$message | ${parts.joinToString(" ")}"
	}

	companion object {
		fun of(clazz: Class<*>): TemporalWorkflowLog = TemporalWorkflowLog(Workflow.getLogger(clazz))

		inline fun <reified T> of(): TemporalWorkflowLog = of(T::class.java)
	}
}
