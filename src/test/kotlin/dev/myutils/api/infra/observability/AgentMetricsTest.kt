package dev.myutils.api.infra.observability

import io.micrometer.prometheusmetrics.PrometheusMeterRegistry
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentMetricsTest {
	@Test
	fun `exports request turn and llm step counters`() {
		val registry = PrometheusMeterRegistry(io.micrometer.prometheusmetrics.PrometheusConfig.DEFAULT)
		val metrics = AgentMetrics(registry)

		metrics.registerMeters()
		metrics.recordReceived("temporal")
		metrics.recordInbound("temporal", "reply", durationMs = 1200, llmSteps = 2)
		metrics.timeLlmStep("temporal") { "ok" }

		val scrape = registry.scrape()
		assertTrue(scrape.contains("agent_requests_total"), scrape)
		assertTrue(scrape.contains("agent_turns_total"), scrape)
		assertTrue(scrape.contains("agent_llm_steps_total"), scrape)
		assertTrue(scrape.contains("outcome=\"received\""), scrape)
		assertTrue(scrape.contains("outcome=\"reply\""), scrape)
	}
}
