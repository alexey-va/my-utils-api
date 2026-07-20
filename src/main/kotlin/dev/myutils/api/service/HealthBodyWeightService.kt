package dev.myutils.api.service

import dev.myutils.api.domain.HealthBodyWeight
import dev.myutils.api.domain.HealthBodyWeightRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.web.dto.HealthBodyWeightDayDto
import dev.myutils.api.web.dto.HealthBodyWeightHistoryResponse
import dev.myutils.api.web.dto.UpsertBodyWeightResponse
import org.slf4j.LoggerFactory
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.math.BigDecimal
import java.math.RoundingMode
import java.time.Instant
import java.time.LocalDate
import java.time.format.DateTimeFormatter

@Service
class HealthBodyWeightService(
	private val userRepository: UserRepository,
	private val bodyWeightRepository: HealthBodyWeightRepository,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val dateFmt = DateTimeFormatter.ofPattern("dd.MM.yyyy")

	@Transactional
	fun upsert(
		weightKg: BigDecimal,
		date: LocalDate,
	): UpsertBodyWeightResponse {
		val normalized = normalizeWeight(weightKg)
		val user = localWorkoutUser()
		val existing = bodyWeightRepository.findByUserIdAndWeightDate(user.id, date)
		val created: Boolean
		if (existing.isPresent) {
			val row = existing.get()
			row.weightKg = normalized
			row.updatedAt = Instant.now()
			bodyWeightRepository.save(row)
			created = false
		} else {
			bodyWeightRepository.save(
				HealthBodyWeight(
					user = user,
					weightDate = date,
					weightKg = normalized,
				),
			)
			created = true
		}

		log.info(
			"DB UPSERT health_body_weight user={} date={} weightKg={} created={}",
			user.email,
			date,
			formatKg(normalized),
			created,
		)

		return UpsertBodyWeightResponse(
			date = date.toString(),
			weightKg = normalized,
			created = created,
		)
	}

	fun history(
		days: Int?,
		today: LocalDate,
	): HealthBodyWeightHistoryResponse {
		val user = localWorkoutUser()
		val rows =
			if (days != null && days > 0) {
				val from = today.minusDays((days - 1).toLong())
				bodyWeightRepository.findByUserIdAndWeightDateBetweenOrderByWeightDateAsc(user.id, from, today)
			} else {
				bodyWeightRepository.findByUserIdOrderByWeightDateDesc(user.id).sortedBy { it.weightDate }
			}

		val dayDtos =
			rows.map { row ->
				HealthBodyWeightDayDto(date = row.weightDate.toString(), weightKg = row.weightKg)
			}

		val latest = bodyWeightRepository.findFirstByUserIdOrderByWeightDateDesc(user.id).orElse(null)

		return HealthBodyWeightHistoryResponse(
			days = dayDtos,
			latestWeightKg = latest?.weightKg,
			latestDate = latest?.weightDate?.toString(),
		)
	}

	fun agentSummary(
		today: LocalDate,
		recentDays: Int = 14,
	): String {
		val user = localWorkoutUser()
		val latest = bodyWeightRepository.findFirstByUserIdOrderByWeightDateDesc(user.id).orElse(null)
		if (latest == null) {
			return "Вес тела ещё не записывали. Можно записать через log_body_weight."
		}

		val from = today.minusDays((recentDays - 1).coerceAtLeast(0).toLong())
		val recent =
			bodyWeightRepository
				.findByUserIdAndWeightDateBetweenOrderByWeightDateAsc(user.id, from, today)
				.takeLast(recentDays.coerceIn(1, 31))

		return buildString {
			appendLine(
				"Последний: ${formatKg(latest.weightKg)} кг (${dateFmt.format(latest.weightDate)})",
			)
			if (recent.size <= 1) {
				return@buildString
			}
			appendLine("Недавние ($recentDays дн.):")
			for (row in recent) {
				appendLine("• ${dateFmt.format(row.weightDate)}: ${formatKg(row.weightKg)} кг")
			}
			val first = recent.first()
			val delta = latest.weightKg.subtract(first.weightKg)
			val sign = if (delta.signum() > 0) "+" else ""
			append("Δ за период: $sign${formatKg(delta)} кг")
		}.trim()
	}

	fun formatLogResult(
		date: LocalDate,
		weightKg: BigDecimal,
		created: Boolean,
	): String {
		val verb = if (created) "Записан" else "Обновлён"
		return "$verb вес тела: ${formatKg(weightKg)} кг (${dateFmt.format(date)})."
	}

	companion object {
		val MIN_KG: BigDecimal = BigDecimal("20")
		val MAX_KG: BigDecimal = BigDecimal("400")

		fun normalizeWeight(raw: BigDecimal): BigDecimal {
			val scaled = raw.setScale(1, RoundingMode.HALF_UP)
			if (scaled < MIN_KG || scaled > MAX_KG) {
				throw ResponseStatusException(
					HttpStatus.BAD_REQUEST,
					"Вес тела должен быть от ${formatKg(MIN_KG)} до ${formatKg(MAX_KG)} кг",
				)
			}
			return scaled
		}

		fun formatKg(kg: BigDecimal): String = kg.stripTrailingZeros().toPlainString()
	}

	private fun localWorkoutUser(): User =
		userRepository
			.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL)
			.orElseThrow {
				ResponseStatusException(
					HttpStatus.INTERNAL_SERVER_ERROR,
					"Local workout user is not configured",
				)
			}
}
