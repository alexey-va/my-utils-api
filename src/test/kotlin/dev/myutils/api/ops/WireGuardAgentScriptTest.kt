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

	@Test
	fun `heartbeat includes optional non-secret geo routing status`() {
		val script = Files.readString(Path.of("ops/wireguard/wireguard-agent.sh"))

		assertThat(script).contains("WIREGUARD_ROUTING_STATUS_FILE")
		assertThat(script).contains("routingStatus: \$routingStatus[0]")
		assertThat(script).contains("mode == \"RU_DIRECT_AWG_DEFAULT\"")
		assertThat(script).doesNotContain("routingStatus.token")
	}

	@Test
	fun `agent reports interval route counters and direct plus Veesp quality probes`() {
		val script = Files.readString(Path.of("ops/wireguard/wireguard-agent.sh"))

		assertThat(script).contains("MYUTILS-WG-TRAFFIC")
		assertThat(script).contains("routingTraffic")
		assertThat(script).contains("ruDownloadBytes")
		assertThat(script).contains("nonRuUploadBytes")
		assertThat(script).contains("routeQuality: \$routeQuality[0]")
		assertThat(script).contains("packetLossPercent")
		assertThat(script).contains("awg show \"\$WIREGUARD_AWG_INTERFACE\" endpoints")
	}
}
