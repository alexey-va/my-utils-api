package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.time.LocalDate
import java.util.Optional
import java.util.UUID

interface HealthBodyWeightRepository : JpaRepository<HealthBodyWeight, UUID> {
	fun findByUserIdAndWeightDate(
		userId: UUID,
		weightDate: LocalDate,
	): Optional<HealthBodyWeight>

	fun findByUserIdAndWeightDateBetweenOrderByWeightDateAsc(
		userId: UUID,
		from: LocalDate,
		to: LocalDate,
	): List<HealthBodyWeight>

	fun findByUserIdOrderByWeightDateDesc(userId: UUID): List<HealthBodyWeight>

	fun findFirstByUserIdOrderByWeightDateDesc(userId: UUID): Optional<HealthBodyWeight>
}
