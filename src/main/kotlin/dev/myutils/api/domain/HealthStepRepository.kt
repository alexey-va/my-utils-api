package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.time.LocalDate
import java.util.Optional
import java.util.UUID

interface HealthStepRepository : JpaRepository<HealthStep, UUID> {
	fun findByUserIdAndStepDate(
		userId: UUID,
		stepDate: LocalDate,
	): Optional<HealthStep>

	fun findByUserIdAndStepDateBetweenOrderByStepDateAsc(
		userId: UUID,
		from: LocalDate,
		to: LocalDate,
	): List<HealthStep>

	fun findByUserIdOrderByStepDateDesc(userId: UUID): List<HealthStep>
}
