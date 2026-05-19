package dev.myutils.api.web

import dev.myutils.api.security.SessionPrincipal
import dev.myutils.api.service.AuthService
import dev.myutils.api.web.dto.LoginRequest
import dev.myutils.api.web.dto.LoginResponse
import dev.myutils.api.web.dto.UserDto
import jakarta.validation.Valid
import org.springframework.security.core.Authentication
import org.springframework.security.core.annotation.AuthenticationPrincipal
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/auth")
class AuthController(
	private val authService: AuthService,
) {
	@PostMapping("/login")
	fun login(@Valid @RequestBody body: LoginRequest): LoginResponse =
		authService.login(body.email, body.password)

	@PostMapping("/logout")
	fun logout(authentication: Authentication) {
		val sessionId = (authentication.details as? SessionPrincipal)?.sessionId ?: return
		authService.logout(sessionId)
	}

	@GetMapping("/me")
	fun me(@AuthenticationPrincipal email: String): UserDto = UserDto(email = email)
}
