package dev.myutils.api.infra.security

import dev.myutils.api.domain.User
import dev.myutils.api.infra.config.MyUtilsProperties
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Test

class JwtServiceTest {
	@Test
	fun `signer can be warmed before the first user session is issued`() {
		val properties =
			MyUtilsProperties(
				jwt =
					MyUtilsProperties.JwtProperties(
						secret = "0123456789abcdef0123456789abcdef",
					),
			)
		val service = JwtService(properties)

		service.warmUp()
		val user = User(email = "dev@example.com", username = "dev", passwordHash = "unused")
		val issued = service.issue(user)

		assertNotNull(issued.token)
		assertEquals(user.id.toString(), service.parseClaims(issued.token)?.subject)
	}
}
