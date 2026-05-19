package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
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
	val email: String,
	@Column(name = "password_hash", nullable = false)
	val passwordHash: String,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)
