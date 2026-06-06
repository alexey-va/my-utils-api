package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.time.LocalDate
import java.util.Optional
import java.util.UUID

interface WorkoutEntryRepository : JpaRepository<WorkoutEntry, UUID> {
	fun findByUserIdOrderByPerformedOnDescCreatedAtDesc(userId: UUID): List<WorkoutEntry>

	fun findByUserIdAndExerciseIdAndPerformedOn(
		userId: UUID,
		exerciseId: UUID,
		performedOn: LocalDate,
	): Optional<WorkoutEntry>

	fun findByUserIdAndExerciseIdOrderByPerformedOnAsc(
		userId: UUID,
		exerciseId: UUID,
	): List<WorkoutEntry>

	fun findByUserIdAndPerformedOnOrderByCreatedAtAsc(
		userId: UUID,
		performedOn: LocalDate,
	): List<WorkoutEntry>

	fun findByUserIdAndPerformedOnBetweenOrderByPerformedOnAscCreatedAtAsc(
		userId: UUID,
		from: LocalDate,
		to: LocalDate,
	): List<WorkoutEntry>
}
