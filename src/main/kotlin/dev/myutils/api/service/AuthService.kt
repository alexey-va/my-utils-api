package dev.myutils.api.service

import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.infra.security.IssuedToken
import dev.myutils.api.infra.security.JwtService
import dev.myutils.api.infra.session.SessionService
import dev.myutils.api.web.dto.LoginResponse
import dev.myutils.api.web.dto.UserDto
import org.springframework.http.HttpStatus
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException
import java.time.Duration

@Service
class AuthService(
	private val userRepository: UserRepository,
	private val passwordEncoder: PasswordEncoder,
	private val jwtService: JwtService,
	private val sessionService: SessionService,
	private val properties: MyUtilsProperties,
) {
	fun login(
		email: String,
		password: String,
	): LoginResponse {
		val user =
			userRepository
				.findByEmailIgnoreCase(email.trim())
				.orElseThrow {
					ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid credentials")
				}

		if (!passwordEncoder.matches(password, user.passwordHash)) {
			throw ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid credentials")
		}

		val issued = jwtService.issue(user.email)
		storeSession(issued)
		return LoginResponse(token = issued.token, user = UserDto(email = user.email))
	}

	fun logout(sessionId: String) {
		sessionService.revoke(sessionId)
	}

	fun validateSession(sessionId: String): Boolean = sessionService.exists(sessionId)

	private fun storeSession(issued: IssuedToken) {
		val ttl = Duration.ofHours(properties.jwt.expirationHours)
		sessionService.store(issued.sessionId, issued.email, ttl)
	}
}
