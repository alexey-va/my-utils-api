package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.EnumType
import jakarta.persistence.Enumerated
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "users")
class User(
	@Id
	val id: UUID = UUID.randomUUID(),
	@Column(nullable = false, unique = true)
	var email: String,
	@Column(nullable = false)
	var username: String = email.substringBefore("@"),
	@Column(name = "password_hash", nullable = false)
	var passwordHash: String,
	@Enumerated(EnumType.STRING)
	@Column(nullable = false)
	var role: UserRole = UserRole.USER,
	@Column(name = "must_change_password", nullable = false)
	var mustChangePassword: Boolean = false,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)

enum class UserRole {
	USER,
	ADMIN,
}
