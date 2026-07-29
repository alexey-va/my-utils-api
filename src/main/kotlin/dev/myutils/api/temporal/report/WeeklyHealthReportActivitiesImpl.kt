package dev.myutils.api.temporal.report

import dev.myutils.api.service.HealthBodyWeightService
import dev.myutils.api.service.HealthStepsService
import dev.myutils.api.service.WeeklyHealthReportRenderer
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component
import java.time.LocalDate
import java.time.format.DateTimeFormatter

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class WeeklyHealthReportActivitiesImpl(
	private val healthStepsService: HealthStepsService,
	private val healthBodyWeightService: HealthBodyWeightService,
	private val renderer: WeeklyHealthReportRenderer,
	private val telegram: TelegramMessenger,
) : WeeklyHealthReportActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun generateAndSend(input: WeeklyHealthReportActivityInput) {
		val reportDate = LocalDate.parse(input.reportDate)
		val lookbackDays = input.lookbackDays.coerceIn(7, 366)
		val from = reportDate.minusDays((lookbackDays - 1).toLong())
		val steps =
			healthStepsService
				.history(lookbackDays, reportDate)
				.days
				.map {
					WeeklyHealthReportRenderer.StepPoint(
						date = LocalDate.parse(it.date),
						steps = it.steps,
					)
				}
		val weights =
			healthBodyWeightService
				.history(lookbackDays, reportDate)
				.days
				.map {
					WeeklyHealthReportRenderer.WeightPoint(
						date = LocalDate.parse(it.date),
						weightKg = it.weightKg.toDouble(),
					)
				}

		val stepsPng = renderer.renderSteps(steps, from, reportDate)
		val weightPng = renderer.renderWeight(weights, from, reportDate)
		val captionDate = captionDateFmt.format(reportDate)

		telegram.sendPhoto(
			input.chatId,
			stepsPng,
			"<b>Шаги · еженедельный отчёт</b>\nДо $captionDate · последние $lookbackDays дней",
		)
		telegram.sendPhoto(
			input.chatId,
			weightPng,
			"<b>Вес · еженедельный отчёт</b>\nДо $captionDate · последние $lookbackDays дней",
		)
		log
			.atInfo()
			.addKeyValue("event_type", "weekly_health_report")
			.addKeyValue("report_chat_id", input.chatId)
			.addKeyValue("report_date", reportDate)
			.addKeyValue("report_lookback_days", lookbackDays)
			.addKeyValue("report_step_points", steps.size)
			.addKeyValue("report_weight_points", weights.size)
			.addKeyValue("report_steps_png_bytes", stepsPng.size)
			.addKeyValue("report_weight_png_bytes", weightPng.size)
			.log("Weekly health report sent")
	}

	private companion object {
		val captionDateFmt: DateTimeFormatter = DateTimeFormatter.ofPattern("dd.MM.yyyy")
	}
}
