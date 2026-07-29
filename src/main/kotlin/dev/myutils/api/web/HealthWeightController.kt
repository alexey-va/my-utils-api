package dev.myutils.api.web

import dev.myutils.api.properties.AppProperties
import dev.myutils.api.service.HealthBodyWeightService
import dev.myutils.api.web.dto.HealthBodyWeightHistoryResponse
import dev.myutils.api.web.dto.UpsertBodyWeightRequest
import dev.myutils.api.web.dto.UpsertBodyWeightResponse
import jakarta.validation.Valid
import org.springframework.http.HttpStatus
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeParseException

@RestController
@RequestMapping("/api/health")
class HealthWeightController(
	private val healthBodyWeightService: HealthBodyWeightService,
) {
	@GetMapping("/weight")
	fun listWeight(
		@RequestParam(required = false) days: Int?,
	): HealthBodyWeightHistoryResponse {
		val today = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))
		return healthBodyWeightService.history(days = days, today = today)
	}

	@PostMapping("/weight")
	fun upsertWeight(
		@Valid @RequestBody request: UpsertBodyWeightRequest,
	): UpsertBodyWeightResponse {
		val today = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))
		val date =
			request.date?.let { rawDate ->
				val normalizedDate =
					rawDate.trim().takeIf { it.isNotEmpty() }
						?: throw invalidDate()
				try {
					LocalDate.parse(normalizedDate)
				} catch (_: DateTimeParseException) {
					throw invalidDate()
				}
			} ?: today
		return healthBodyWeightService.upsert(request.weightKg, date)
	}

	private fun invalidDate(): ResponseStatusException =
		ResponseStatusException(
			HttpStatus.BAD_REQUEST,
			"Неверная дата date (YYYY-MM-DD)",
		)
}
