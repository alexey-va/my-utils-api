package dev.myutils.api.wireguard

class Ipv4Cidr private constructor(
	private val network: Long,
	val prefixLength: Int,
) {
	val value: String = "${formatAddress(network)}/$prefixLength"
	val lastUsableHostOffset: Int = ((1L shl (32 - prefixLength)) - 2).toInt()

	fun hostAddress(offset: Int): String {
		require(offset in 1..lastUsableHostOffset) { "Host offset is outside the usable CIDR range" }
		return formatAddress(network + offset)
	}

	fun contains(address: String): Boolean {
		val parsed = runCatching { parseAddress(address) }.getOrNull() ?: return false
		val mask = prefixMask(prefixLength)
		return parsed and mask == network
	}

	companion object {
		fun parse(value: String): Ipv4Cidr {
			val parts = value.trim().split('/')
			require(parts.size == 2) { "Client network must be an IPv4 CIDR" }
			val address = parseAddress(parts[0])
			val prefix = parts[1].toIntOrNull()
			require(prefix != null && prefix in 16..29) { "Client CIDR prefix must be between /16 and /29" }
			require(isPrivateAddress(address)) { "Client CIDR must use an RFC1918 private address" }
			val network = address and prefixMask(prefix)
			require(isPrivateAddress(network)) { "Client CIDR must remain inside an RFC1918 range" }
			return Ipv4Cidr(network, prefix)
		}

		private fun isPrivateAddress(address: Long): Boolean =
			address in parseAddress("10.0.0.0")..parseAddress("10.255.255.255") ||
				address in parseAddress("172.16.0.0")..parseAddress("172.31.255.255") ||
				address in parseAddress("192.168.0.0")..parseAddress("192.168.255.255")

		private fun prefixMask(prefix: Int): Long =
			if (prefix == 0) 0 else (0xffffffffL shl (32 - prefix)) and 0xffffffffL

		private fun parseAddress(value: String): Long {
			val octets = value.split('.')
			require(octets.size == 4) { "Invalid IPv4 address" }
			return octets.fold(0L) { result, item ->
				require(item.isNotEmpty() && (item == "0" || !item.startsWith('0'))) { "Invalid IPv4 octet" }
				val octet = item.toIntOrNull()
				require(octet != null && octet in 0..255) { "Invalid IPv4 octet" }
				(result shl 8) or octet.toLong()
			}
		}

		private fun formatAddress(address: Long): String =
			listOf(24, 16, 8, 0)
				.joinToString(".") { shift -> ((address shr shift) and 0xff).toString() }
	}
}
