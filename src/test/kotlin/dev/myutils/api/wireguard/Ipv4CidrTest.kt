package dev.myutils.api.wireguard

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class Ipv4CidrTest {
	@Test
	fun `normalizes a private network and allocates usable hosts`() {
		val cidr = Ipv4Cidr.parse("10.89.0.41/24")

		assertEquals("10.89.0.0/24", cidr.value)
		assertEquals("10.89.0.1", cidr.hostAddress(1))
		assertEquals("10.89.0.2", cidr.hostAddress(2))
		assertEquals(254, cidr.lastUsableHostOffset)
		assertTrue(cidr.contains("10.89.0.254"))
		assertFalse(cidr.contains("10.89.1.1"))
	}

	@Test
	fun `supports all RFC1918 ranges`() {
		assertEquals("10.0.0.0/16", Ipv4Cidr.parse("10.0.0.0/16").value)
		assertEquals("172.16.8.0/24", Ipv4Cidr.parse("172.16.8.0/24").value)
		assertEquals("192.168.4.0/24", Ipv4Cidr.parse("192.168.4.0/24").value)
	}

	@Test
	fun `rejects public invalid and impractical client networks`() {
		listOf(
			"8.8.8.0/24",
			"10.0.0.0/8",
			"10.0.0.0/30",
			"10.0.0.0/33",
			"10.0.0/24",
			"not-a-cidr",
		).forEach { value ->
			assertThrows(IllegalArgumentException::class.java) {
				Ipv4Cidr.parse(value)
			}
		}
	}

	@Test
	fun `rejects network broadcast and out of range host offsets`() {
		val cidr = Ipv4Cidr.parse("10.89.0.0/24")

		listOf(0, 255, 256).forEach { offset ->
			assertThrows(IllegalArgumentException::class.java) {
				cidr.hostAddress(offset)
			}
		}
	}
}
