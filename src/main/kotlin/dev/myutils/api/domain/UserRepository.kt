package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.Optional
import java.util.UUID

interface UserRepository : JpaRepository<User, UUID> {
	fun findByEmailIgnoreCase(email: String): Optional<User>

	fun findByUsernameIgnoreCase(username: String): Optional<User>

	fun findFirstByUsernameIgnoreCaseOrEmailIgnoreCase(
		username: String,
		email: String,
	): Optional<User>

	fun existsByUsernameIgnoreCase(username: String): Boolean

	fun existsByUsernameIgnoreCaseAndIdNot(
		username: String,
		id: UUID,
	): Boolean

	fun existsByEmailIgnoreCase(email: String): Boolean

	fun existsByEmailIgnoreCaseAndIdNot(
		email: String,
		id: UUID,
	): Boolean

	fun existsByRole(role: UserRole): Boolean
}
