package dev.myutils.api.web

import com.fasterxml.jackson.databind.JsonNode
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.service.AppleHealthStepsParser
import dev.myutils.api.service.HealthStepsService
import dev.myutils.api.web.dto.HealthStepsHistoryResponse
import dev.myutils.api.web.dto.StepsIngestResponse
import dev.myutils.api.web.dto.StepsParsedDto
import jakarta.servlet.http.HttpServletRequest
import org.slf4j.LoggerFactory
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

@RestController
@RequestMapping("/api/health")
class StepsIngestController(
	private val healthStepsService: HealthStepsService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@GetMapping("/steps")
	fun listSteps(
		@RequestParam(required = false) days: Int?,
	): HealthStepsHistoryResponse {
		val today = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))
		return healthStepsService.history(days = days, today = today)
	}

	@PostMapping("/steps")
	fun ingestSteps(
		@RequestBody(required = false) body: JsonNode?,
		request: HttpServletRequest,
	): StepsIngestResponse {
		val headers =
			request.headerNames
				.toList()
				.sorted()
				.associateWith { name -> request.getHeader(name).orEmpty() }

		val today = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))
		val parsed =
			try {
				AppleHealthStepsParser.parse(body, today)
			} catch (error: RuntimeException) {
				log.warn(
					"Steps ingest parse failed from={} body={}",
					request.remoteAddr,
					body,
					error,
				)
				throw ResponseStatusException(
					HttpStatus.BAD_REQUEST,
					"Неверные данные шагов",
					error,
				)
			}

		val savedDays = parsed?.let { healthStepsService.upsertParsed(it) }

		log.info(
			"Steps ingest from={} contentType={} headers={} body={} parsed={} savedDays={}",
			request.remoteAddr,
			request.contentType,
			headers,
			body,
			parsed?.let {
				"source=${it.source} days=${it.days.size} today=${it.today?.steps}"
			},
			savedDays,
		)

		return StepsIngestResponse(
			ok = true,
			received = body,
			parsed = parsed?.let { StepsParsedDto.from(it) },
			savedDays = savedDays,
		)
	}
}
