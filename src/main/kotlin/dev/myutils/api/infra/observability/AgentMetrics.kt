package dev.myutils.api.infra.observability

import io.micrometer.core.instrument.MeterRegistry
import org.springframework.stereotype.Component
import java.time.Duration

@Component
class AgentMetrics(
	private val registry: MeterRegistry,
) {
	fun recordRequest(
		path: String,
		outcome: String,
	) {
		registry.counter("agent.requests.total", "path", path, "outcome", outcome).increment()
	}

	fun recordTurn(
		path: String,
		outcome: String,
		durationMs: Long,
		llmSteps: Int,
	) {
		registry
			.timer("agent.turn.duration", "path", path, "outcome", outcome)
			.record(Duration.ofMillis(durationMs))
		registry.counter("agent.turns.total", "path", path, "outcome", outcome).increment()
		if (llmSteps > 0) {
			registry.summary("agent.turn.llm_steps", "path", path, "outcome", outcome).record(llmSteps.toDouble())
		}
	}

	fun <T> timeTurn(
		path: String,
		block: () -> T,
	): T = registry.timer("agent.turn.duration", "path", path).recordCallable(block)!!

	fun <T> timeLlmStep(
		path: String,
		block: () -> T,
	): T {
		registry.counter("agent.llm.steps.total", "path", path).increment()
		return registry.timer("agent.llm.step.duration", "path", path).recordCallable(block)!!
	}

	fun recordLlmToolRequests(
		path: String,
		count: Int,
	) {
		if (count > 0) {
			registry.summary("agent.llm.tool_requests", "path", path).record(count.toDouble())
		}
	}

	fun <T> timeTool(
		toolName: String,
		path: String,
		block: () -> T,
	): T =
		try {
			val result =
				registry
					.timer("agent.tool.duration", "tool", toolName, "path", path)
					.recordCallable(block)!!
			registry
				.counter("agent.tool.calls.total", "tool", toolName, "path", path, "status", "success")
				.increment()
			result
		} catch (ex: Exception) {
			registry
				.counter("agent.tool.calls.total", "tool", toolName, "path", path, "status", "error")
				.increment()
			throw ex
		}
}
