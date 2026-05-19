package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.Optional
import java.util.UUID

interface ExerciseRepository : JpaRepository<Exercise, UUID> {
	fun findByUserIdOrderByNameAsc(userId: UUID): List<Exercise>

	fun findByUserIdAndNameIgnoreCase(
		userId: UUID,
		name: String,
	): Optional<Exercise>

	fun existsByUserIdAndNameIgnoreCase(
		userId: UUID,
		name: String,
	): Boolean

	fun existsByUserIdAndNameIgnoreCaseAndIdNot(
		userId: UUID,
		name: String,
		id: UUID,
	): Boolean
}
