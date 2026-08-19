package dev.myutils.api.ops

import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test
import java.nio.file.Path

class GeoRoutingScriptsTest {
	@Test
	fun `renderer produces deterministic atomic nft transaction for safe public IPv4 prefixes`() {
		val result =
			runProcess(
				listOf(
					"python3",
					"ops/wireguard/render-geo-prefixes.py",
					"--minimum-prefixes",
					"2",
					"--maximum-prefixes",
					"10",
				),
				"31.13.24.0/21\n5.255.192.0/18\n5.255.192.0/18\n",
			)

		assertThat(result.exitCode).isZero()
		assertThat(result.output).isEqualTo(
			"flush set ip myutils_wg_geo ru_ipv4\n" +
				"add element ip myutils_wg_geo ru_ipv4 { 5.255.192.0/18, 31.13.24.0/21 }\n",
		)
	}

	@Test
	fun `renderer rejects direct-route escape hatches and non-public ranges`() {
		for (unsafe in listOf("0.0.0.0/0", "10.0.0.0/8", "127.0.0.0/8", "224.0.0.0/4")) {
			val result =
				runProcess(
					listOf(
						"python3",
						"ops/wireguard/render-geo-prefixes.py",
						"--minimum-prefixes",
						"1",
						"--maximum-prefixes",
						"10",
					),
					"$unsafe\n",
				)

			assertThat(result.exitCode).isNotZero()
			assertThat(result.output).contains("unsafe IPv4 network")
		}
	}

	@Test
	fun `geo installer plan is explicit and non-mutating`() {
		val result =
			runProcess(
				listOf(
					"bash",
					"ops/wireguard/install-geo-routing.sh",
					"--client-cidr",
					"10.89.0.0/24",
					"--ingress-interface",
					"wg-users",
					"--direct-egress-interface",
					"eth0",
				),
			)

		assertThat(result.exitCode).isZero()
		assertThat(result.output).contains("Plan only; no host changes were made")
		assertThat(result.output).contains("priority 1088")
		assertThat(result.output).contains("unmarked traffic remains on AWG table 51889")
	}

	private fun runProcess(
		command: List<String>,
		input: String = "",
	): ProcessResult {
		val process =
			ProcessBuilder(command)
				.directory(Path.of(".").toFile())
				.redirectErrorStream(true)
				.start()
		process.outputStream.bufferedWriter().use { it.write(input) }
		val output = process.inputStream.bufferedReader().use { it.readText() }
		return ProcessResult(process.waitFor(), output)
	}

	private data class ProcessResult(
		val exitCode: Int,
		val output: String,
	)
}
