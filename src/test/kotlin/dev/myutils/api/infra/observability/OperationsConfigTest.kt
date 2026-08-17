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
}
