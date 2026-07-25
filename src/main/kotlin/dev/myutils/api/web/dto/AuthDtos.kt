package dev.myutils.api.web.dto

import jakarta.validation.constraints.Email
import jakarta.validation.constraints.NotBlank
import jakarta.validation.constraints.Pattern
import jakarta.validation.constraints.Size
import java.util.UUID

data class LoginRequest(
	@field:NotBlank val login: String,
	@field:NotBlank val password: String,
)

data class RegisterRequest(
	@field:NotBlank
	@field:Size(min = 3, max = 32)
	@field:Pattern(
		regexp = "^[A-Za-z0-9_.-]+$",
		message = "Username may contain letters, numbers, dot, dash and underscore",
	)
	val username: String,
	@field:Email @field:NotBlank val email: String,
	@field:Size(min = 8, max = 128) val password: String,
)

data class UpdateCredentialsRequest(
	@field:NotBlank val currentPassword: String,
	@field:Size(min = 3, max = 32)
	@field:Pattern(
		regexp = "^[A-Za-z0-9_.-]+$",
		message = "Username may contain letters, numbers, dot, dash and underscore",
	)
	val username: String? = null,
	@field:Email val email: String? = null,
	@field:Size(min = 8, max = 128) val newPassword: String? = null,
)

data class LoginResponse(
	val token: String,
	val user: UserDto,
)

data class UserDto(
	val id: UUID,
	val username: String,
	val email: String,
	val role: String,
	val mustChangePassword: Boolean,
)
