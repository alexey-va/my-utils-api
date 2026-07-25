package dev.myutils.api.service

import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.infra.security.IssuedToken
import dev.myutils.api.infra.security.JwtService
import dev.myutils.api.infra.session.SessionService
import dev.myutils.api.web.dto.LoginResponse
import dev.myutils.api.web.dto.RegisterRequest
import dev.myutils.api.web.dto.UpdateCredentialsRequest
import dev.myutils.api.web.dto.UserDto
import org.springframework.http.HttpStatus
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException
import java.time.Duration
import java.util.UUID

@Service
class AuthService(
	private val userRepository: UserRepository,
	private val passwordEncoder: PasswordEncoder,
	private val jwtService: JwtService,
	private val sessionService: SessionService,
	private val properties: MyUtilsProperties,
) {
	fun login(
		login: String,
		password: String,
	): LoginResponse {
		val user =
			userRepository
				.findFirstByUsernameIgnoreCaseOrEmailIgnoreCase(login.trim(), login.trim())
				.orElseThrow {
					ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid credentials")
				}

		if (!passwordEncoder.matches(password, user.passwordHash)) {
			throw ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid credentials")
		}

		return issueSession(user)
	}

	fun register(request: RegisterRequest): LoginResponse {
		val username = normalizeUsername(request.username)
		val email = normalizeEmail(request.email)
		if (userRepository.existsByUsernameIgnoreCase(username)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Username is already taken")
		}
		if (userRepository.existsByEmailIgnoreCase(email)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Email is already registered")
		}
		val user =
			userRepository.save(
				User(
					username = username,
					email = email,
					passwordHash = passwordEncoder.encode(request.password),
					role = UserRole.USER,
				),
			)
		return issueSession(user)
	}

	fun profile(userId: UUID): UserDto = toUserDto(requireUser(userId))

	fun updateCredentials(
		userId: UUID,
		request: UpdateCredentialsRequest,
	): LoginResponse {
		val user = requireUser(userId)
		if (!passwordEncoder.matches(request.currentPassword, user.passwordHash)) {
			throw ResponseStatusException(HttpStatus.UNAUTHORIZED, "Current password is incorrect")
		}
		val username = request.username?.trim()?.takeIf(String::isNotEmpty)?.let(::normalizeUsername)
		val email = request.email?.trim()?.takeIf(String::isNotEmpty)?.let(::normalizeEmail)
		val newPassword = request.newPassword?.takeIf(String::isNotBlank)
		if (username == null && email == null && newPassword == null) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "No credential changes supplied")
		}
		if (username != null && userRepository.existsByUsernameIgnoreCaseAndIdNot(username, user.id)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Username is already taken")
		}
		if (email != null && userRepository.existsByEmailIgnoreCaseAndIdNot(email, user.id)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Email is already registered")
		}

		username?.let { user.username = it }
		email?.let { user.email = it }
		newPassword?.let {
			user.passwordHash = passwordEncoder.encode(it)
			user.mustChangePassword = false
		}
		val saved = userRepository.save(user)
		sessionService.revokeUserSessions(saved.id)
		return issueSession(saved)
	}

	fun logout(sessionId: String) {
		sessionService.revoke(sessionId)
	}

	private fun issueSession(user: User): LoginResponse {
		val issued = jwtService.issue(user)
		storeSession(issued, user.id)
		return LoginResponse(token = issued.token, user = toUserDto(user))
	}

	private fun storeSession(
		issued: IssuedToken,
		userId: UUID,
	) {
		val ttl = Duration.ofHours(properties.jwt.expirationHours)
		sessionService.store(issued.sessionId, userId, ttl)
	}

	private fun requireUser(userId: UUID): User =
		userRepository
			.findById(userId)
			.orElseThrow { ResponseStatusException(HttpStatus.UNAUTHORIZED, "Account not found") }

	private fun normalizeUsername(raw: String): String = raw.trim().lowercase()

	private fun normalizeEmail(raw: String): String = raw.trim().lowercase()

	private fun toUserDto(user: User) =
		UserDto(
			id = user.id,
			username = user.username,
			email = user.email,
			role = user.role.name,
			mustChangePassword = user.mustChangePassword,
		)
}
