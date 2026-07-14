package dev.myutils.api.service

import dev.myutils.api.domain.HealthStep
import dev.myutils.api.domain.HealthStepRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.web.dto.HealthStepDayDto
import dev.myutils.api.web.dto.HealthStepsHistoryResponse
import org.slf4j.LoggerFactory
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate

@Service
class HealthStepsService(
	private val userRepository: UserRepository,
	private val healthStepRepository: HealthStepRepository,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@Transactional
	fun upsertParsed(parsed: AppleHealthStepsParser.Parsed): Int {
		val user = localWorkoutUser()
		var changed = 0

		for (day in parsed.days) {
			val existing = healthStepRepository.findByUserIdAndStepDate(user.id, day.date)
			if (existing.isPresent) {
				val row = existing.get()
				if (row.steps != day.steps) {
					row.steps = day.steps
					row.updatedAt = java.time.Instant.now()
					healthStepRepository.save(row)
					changed++
				}
			} else {
				healthStepRepository.save(
					HealthStep(
						user = user,
						stepDate = day.date,
						steps = day.steps,
					),
				)
				changed++
			}
		}

		log.info(
			"DB UPSERT health_steps user={} days={} changed={} today={}",
			user.email,
			parsed.days.size,
			changed,
			parsed.today?.steps,
		)

		return changed
	}

	fun history(
		days: Int?,
		today: LocalDate,
	): HealthStepsHistoryResponse {
		val user = localWorkoutUser()
		val rows =
			if (days != null && days > 0) {
				val from = today.minusDays((days - 1).toLong())
				healthStepRepository.findByUserIdAndStepDateBetweenOrderByStepDateAsc(user.id, from, today)
			} else {
				healthStepRepository.findByUserIdOrderByStepDateDesc(user.id).sortedBy { it.stepDate }
			}

		val dayDtos =
			rows.map { row ->
				HealthStepDayDto(date = row.stepDate.toString(), steps = row.steps)
			}

		val todaySteps = rows.lastOrNull { it.stepDate == today }?.steps

		return HealthStepsHistoryResponse(days = dayDtos, todaySteps = todaySteps)
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
