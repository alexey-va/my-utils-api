package dev.myutils.api.ops

import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test
import java.nio.file.Files
import java.nio.file.Path
import java.util.regex.Pattern

class WireGuardAgentScriptTest {
	@Test
	fun `desired state validator accepts private IPv4 peer addresses`() {
		val script = Files.readString(Path.of("ops/wireguard/wireguard-agent.sh"))
		val marker = "(.allowedIp | type == \"string\" and test(\""
		val jqPattern = script.substringAfter(marker).substringBefore("\"))")
		val pattern = Pattern.compile(jqPattern.replace("\\\\", "\\"))

		assertThat(pattern.matcher("10.89.0.2/32").matches()).isTrue()
		assertThat(pattern.matcher("172.16.0.2/32").matches()).isTrue()
		assertThat(pattern.matcher("192.168.10.2/32").matches()).isTrue()
		assertThat(pattern.matcher("8.8.8.8/32").matches()).isFalse()
		assertThat(pattern.matcher("10.89.0.0/24").matches()).isFalse()
	}
}
