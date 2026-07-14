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
@Table(name = "health_steps")
class HealthStep(
	@Id
	val id: UUID = UUID.randomUUID(),
	@ManyToOne(fetch = FetchType.LAZY, optional = false)
	@JoinColumn(name = "user_id", nullable = false)
	val user: User,
	@Column(name = "step_date", nullable = false)
	val stepDate: LocalDate,
	@Column(nullable = false)
	var steps: Int,
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
)
