package dev.myutils.api.web

import com.fasterxml.jackson.databind.JsonNode
import dev.myutils.api.web.dto.StepsIngestResponse
import jakarta.servlet.http.HttpServletRequest
import org.slf4j.LoggerFactory
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/health")
class StepsIngestController {
	private val log = LoggerFactory.getLogger(javaClass)

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

		log.info(
			"Steps ingest from={} contentType={} headers={} body={}",
			request.remoteAddr,
			request.contentType,
			headers,
			body,
		)

		return StepsIngestResponse(ok = true, received = body)
	}
}
