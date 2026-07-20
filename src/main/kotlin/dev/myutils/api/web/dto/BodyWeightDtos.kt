package dev.myutils.api.web.dto

import jakarta.validation.constraints.NotNull
import java.math.BigDecimal

data class HealthBodyWeightDayDto(
	val date: String,
	val weightKg: BigDecimal,
)

data class HealthBodyWeightHistoryResponse(
	val days: List<HealthBodyWeightDayDto>,
	val latestWeightKg: BigDecimal?,
	val latestDate: String?,
)

data class UpsertBodyWeightRequest(
	@field:NotNull val weightKg: BigDecimal,
	val date: String? = null,
)

data class UpsertBodyWeightResponse(
	val date: String,
	val weightKg: BigDecimal,
	val created: Boolean,
)
