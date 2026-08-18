package dev.myutils.api.infra.observability

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.nio.file.Files
import java.nio.file.Path

class OperationsConfigTest {
	@Test
	fun `ci keeps the shared production host inside its memory budget`() {
		val workflow = Files.readString(Path.of(".woodpecker.yml"))
		val gradleProperties = Files.readString(Path.of("gradle.properties"))
		val dockerfile = Files.readString(Path.of("Dockerfile"))

		assertTrue(workflow.contains("image: gradle:9.4.1-jdk21"), workflow)
		val compileCommand = "      - gradle --no-daemon --max-workers=1 testClasses\n"
		val testCommand = "      - gradle --no-daemon --max-workers=1 test\n"
		assertTrue(workflow.contains(compileCommand), workflow)
		assertTrue(workflow.contains(testCommand), workflow)
		assertTrue(
			workflow.indexOf(compileCommand) < workflow.indexOf(testCommand),
			workflow,
		)
		assertTrue(gradleProperties.contains("org.gradle.jvmargs=-Xmx384m -XX:MaxMetaspaceSize=320m"), gradleProperties)
		assertTrue(gradleProperties.contains("org.gradle.workers.max=1"), gradleProperties)
		assertTrue(gradleProperties.contains("kotlin.compiler.execution.strategy=in-process"), gradleProperties)
		assertTrue(
			dockerfile.contains("COPY build.gradle.kts settings.gradle.kts gradle.properties ./"),
			dockerfile,
		)
	}

	@Test
	fun `swap occupancy only warns at sustained incident levels`() {
		val dashboardPath = Path.of(
			"observability/config/grafana/provisioning/dashboards/root/metal-status.json",
		)
		val dashboard = jacksonObjectMapper().readTree(dashboardPath.toFile())
		val panel = dashboard["panels"].first { it["title"].asText() == "SWAP Used" }
		val steps = panel["fieldConfig"]["defaults"]["thresholds"]["steps"]

		assertEquals(70, steps[1]["value"].asInt())
		assertEquals(90, steps[2]["value"].asInt())
		assertTrue(panel["description"].asText().contains("Pressure → Mem"))
	}

	@Test
	fun `prometheus keeps every RusCrafting metrics scrape target`() {
		val prometheus = Files.readString(Path.of("observability/config/prometheus/prometheus.yml"))
		val arc = prometheus.substringAfter("  - job_name: mc-arc\n")
			.substringBefore("\n  - job_name:")
		val proxyArc = prometheus.substringAfter("  - job_name: mc-proxyarc\n")
			.substringBefore("\n  - job_name:")

		for ((serverName, instance) in listOf(
			"spawn" to "178.44.115.21:26804",
			"survival" to "178.44.115.21:26805",
			"parkour" to "178.44.115.21:26806",
		)) {
			assertTrue(arc.contains("targets: [\"$instance\"]"), arc)
			assertTrue(arc.contains("server_name: $serverName"), arc)
		}
		assertTrue(proxyArc.contains("metrics_path: /proxyarc/metrics"), proxyArc)
		assertTrue(proxyArc.contains("targets: [\"31.44.9.177:9100\"]"), proxyArc)
		assertTrue(proxyArc.contains("server_name: velocity"), proxyArc)
	}
}
