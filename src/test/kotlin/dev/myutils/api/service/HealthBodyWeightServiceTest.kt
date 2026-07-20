package dev.myutils.api.service

import dev.myutils.api.domain.HealthBodyWeight
import dev.myutils.api.domain.HealthBodyWeightRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever
import java.math.BigDecimal
import java.time.LocalDate
import java.util.Optional
import java.util.UUID

class HealthBodyWeightServiceTest {
	private val userId = UUID.randomUUID()
	private val user =
		User(
			id = userId,
			email = WorkoutService.LOCAL_WORKOUT_EMAIL,
			passwordHash = "hash",
		)
	private val userRepository: UserRepository = mock()
	private val bodyWeightRepository: HealthBodyWeightRepository = mock()
	private val service = HealthBodyWeightService(userRepository, bodyWeightRepository)

	@Test
	fun `upsert inserts new day`() {
		val date = LocalDate.parse("2026-07-20")
		whenever(userRepository.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL))
			.thenReturn(Optional.of(user))
		whenever(bodyWeightRepository.findByUserIdAndWeightDate(userId, date))
			.thenReturn(Optional.empty())
		whenever(bodyWeightRepository.save(any())).thenAnswer { it.arguments[0] as HealthBodyWeight }

		val result = service.upsert(BigDecimal("82.5"), date)

		assertTrue(result.created)
		assertEquals(BigDecimal("82.5"), result.weightKg)
		assertEquals(date.toString(), result.date)
		verify(bodyWeightRepository).save(any())
	}

	@Test
	fun `upsert updates existing day`() {
		val date = LocalDate.parse("2026-07-20")
		val existing =
			HealthBodyWeight(
				user = user,
				weightDate = date,
				weightKg = BigDecimal("81.0"),
			)
		whenever(userRepository.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL))
			.thenReturn(Optional.of(user))
		whenever(bodyWeightRepository.findByUserIdAndWeightDate(userId, date))
			.thenReturn(Optional.of(existing))
		whenever(bodyWeightRepository.save(any())).thenAnswer { it.arguments[0] as HealthBodyWeight }

		val result = service.upsert(BigDecimal("82.0"), date)

		assertFalse(result.created)
		assertEquals(BigDecimal("82.0"), existing.weightKg)
		verify(bodyWeightRepository).save(existing)
	}

	@Test
	fun `history returns latest`() {
		val today = LocalDate.parse("2026-07-20")
		val rows =
			listOf(
				HealthBodyWeight(user = user, weightDate = today.minusDays(1), weightKg = BigDecimal("81.5")),
				HealthBodyWeight(user = user, weightDate = today, weightKg = BigDecimal("82.0")),
			)
		whenever(userRepository.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL))
			.thenReturn(Optional.of(user))
		whenever(
			bodyWeightRepository.findByUserIdAndWeightDateBetweenOrderByWeightDateAsc(
				userId,
				today.minusDays(6),
				today,
			),
		).thenReturn(rows)
		whenever(bodyWeightRepository.findFirstByUserIdOrderByWeightDateDesc(userId))
			.thenReturn(Optional.of(rows.last()))

		val history = service.history(days = 7, today = today)

		assertEquals(2, history.days.size)
		assertEquals(BigDecimal("82.0"), history.latestWeightKg)
		assertEquals(today.toString(), history.latestDate)
	}
}
