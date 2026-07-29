package dev.myutils.api.temporal.report

import dev.myutils.api.service.HealthBodyWeightService
import dev.myutils.api.service.HealthStepsService
import dev.myutils.api.service.WeeklyHealthReportRenderer
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.web.dto.HealthBodyWeightDayDto
import dev.myutils.api.web.dto.HealthBodyWeightHistoryResponse
import dev.myutils.api.web.dto.HealthStepDayDto
import dev.myutils.api.web.dto.HealthStepsHistoryResponse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.argumentCaptor
import org.mockito.kotlin.eq
import org.mockito.kotlin.mock
import org.mockito.kotlin.times
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever
import java.math.BigDecimal
import java.time.LocalDate

class WeeklyHealthReportActivitiesImplTest {
	private val stepsService: HealthStepsService = mock()
	private val weightService: HealthBodyWeightService = mock()
	private val telegram: TelegramMessenger = mock()

	@Test
	fun `builds and sends two png reports from health history`() {
		val reportDate = LocalDate.of(2026, 7, 26)
		whenever(stepsService.history(90, reportDate))
			.thenReturn(
				HealthStepsHistoryResponse(
					days =
						listOf(
							HealthStepDayDto("2026-07-24", 8_500),
							HealthStepDayDto("2026-07-25", 11_200),
							HealthStepDayDto("2026-07-26", 9_900),
						),
					todaySteps = 9_900,
				),
			)
		whenever(weightService.history(90, reportDate))
			.thenReturn(
				HealthBodyWeightHistoryResponse(
					days =
						listOf(
							HealthBodyWeightDayDto("2026-06-01", BigDecimal("84.2")),
							HealthBodyWeightDayDto("2026-07-26", BigDecimal("83.1")),
						),
					latestWeightKg = BigDecimal("83.1"),
					latestDate = "2026-07-26",
				),
			)
		val activity =
			WeeklyHealthReportActivitiesImpl(
				healthStepsService = stepsService,
				healthBodyWeightService = weightService,
				renderer = WeeklyHealthReportRenderer(),
				telegram = telegram,
			)

		activity.generateAndSend(
			WeeklyHealthReportActivityInput(
				chatId = 42L,
				reportDate = reportDate.toString(),
				lookbackDays = 90,
			),
		)

		val pngs = argumentCaptor<ByteArray>()
		verify(telegram, times(2)).sendPhoto(eq(42L), pngs.capture(), any())
		assertTrue(pngs.allValues.all { png -> png.size > 10_000 })
		assertTrue(pngs.allValues.all { png -> png.take(4) == listOf(0x89.toByte(), 0x50, 0x4e, 0x47) })
	}
}
