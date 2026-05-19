package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.Optional
import java.util.UUID

interface UserRepository : JpaRepository<User, UUID> {
	fun findByEmailIgnoreCase(email: String): Optional<User>
}
