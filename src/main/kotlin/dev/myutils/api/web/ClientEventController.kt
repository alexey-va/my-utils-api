package dev.myutils.api.web

import dev.myutils.api.service.ClientEventLoggingService
import jakarta.servlet.http.HttpServletRequest
import org.springframework.http.HttpStatus
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/client-events")
class ClientEventController(
	private val clientEventLoggingService: ClientEventLoggingService,
) {
	@PostMapping
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun ingest(
		@RequestBody(required = false) body: String?,
		request: HttpServletRequest,
	) {
		clientEventLoggingService.logBatch(
			body = body,
			origin = request.getHeader("Origin"),
		)
	}
}
