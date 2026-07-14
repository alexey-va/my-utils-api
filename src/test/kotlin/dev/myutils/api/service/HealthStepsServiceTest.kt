package dev.myutils.api.service

import dev.myutils.api.domain.HealthStep
import dev.myutils.api.domain.HealthStepRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever
import java.time.LocalDate
import java.util.Optional
import java.util.UUID

class HealthStepsServiceTest {
	private val userId = UUID.randomUUID()
	private val user =
		User(
			id = userId,
			email = WorkoutService.LOCAL_WORKOUT_EMAIL,
			passwordHash = "hash",
		)
	private val userRepository: UserRepository = mock()
	private val healthStepRepository: HealthStepRepository = mock()
	private val service = HealthStepsService(userRepository, healthStepRepository)

	@Test
	fun `upsert inserts new days`() {
		val today = LocalDate.parse("2026-07-14")
		val parsed =
			AppleHealthStepsParser.Parsed(
				source = "apple-shortcut-multiline",
				days =
					listOf(
						AppleHealthStepsParser.Day(LocalDate.parse("2026-07-13"), 5000),
						AppleHealthStepsParser.Day(today, 8065),
					),
			)

		whenever(userRepository.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL))
			.thenReturn(Optional.of(user))
		whenever(healthStepRepository.findByUserIdAndStepDate(userId, parsed.days[0].date))
			.thenReturn(Optional.empty())
		whenever(healthStepRepository.findByUserIdAndStepDate(userId, parsed.days[1].date))
			.thenReturn(Optional.empty())
		whenever(healthStepRepository.save(any())).thenAnswer { it.arguments[0] as HealthStep }

		val changed = service.upsertParsed(parsed)

		assertEquals(2, changed)
		verify(healthStepRepository, org.mockito.kotlin.times(2)).save(any())
	}

	@Test
	fun `upsert skips unchanged day`() {
		val today = LocalDate.parse("2026-07-14")
		val existing =
			HealthStep(
				user = user,
				stepDate = today,
				steps = 8065,
			)
		val parsed =
			AppleHealthStepsParser.Parsed(
				source = "apple-shortcut-multiline",
				days = listOf(AppleHealthStepsParser.Day(today, 8065)),
			)

		whenever(userRepository.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL))
			.thenReturn(Optional.of(user))
		whenever(healthStepRepository.findByUserIdAndStepDate(userId, today))
			.thenReturn(Optional.of(existing))

		val changed = service.upsertParsed(parsed)

		assertEquals(0, changed)
		verify(healthStepRepository, never()).save(any())
	}
}
