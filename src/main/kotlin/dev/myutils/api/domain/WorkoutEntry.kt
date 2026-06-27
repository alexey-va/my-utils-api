package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.FetchType
import jakarta.persistence.Id
import jakarta.persistence.JoinColumn
import jakarta.persistence.ManyToOne
import jakarta.persistence.Table
import java.time.Instant
import java.time.LocalDate
import java.util.UUID

@Entity
@Table(name = "workout_entries")
class WorkoutEntry(
	@Id
	val id: UUID = UUID.randomUUID(),
	@ManyToOne(fetch = FetchType.LAZY, optional = false)
	@JoinColumn(name = "user_id", nullable = false)
	val user: User,
	@ManyToOne(fetch = FetchType.LAZY, optional = false)
	@JoinColumn(name = "exercise_id", nullable = false)
	val exercise: Exercise,
	@Column(name = "performed_on", nullable = false)
	val performedOn: LocalDate,
	@Column(name = "weight_kg", nullable = false)
	val weightKg: Int,
	@Column(name = "set_count", nullable = false)
	val setCount: Int,
	@Column(name = "reps_per_set", nullable = false)
	val repsPerSet: Int,
	@Column(name = "max_reps", nullable = false)
	val maxReps: Int,
	@Column(name = "set_reps")
	val setReps: String? = null,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)
