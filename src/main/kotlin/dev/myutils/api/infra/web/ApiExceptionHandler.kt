package dev.myutils.api.infra.web

import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.web.bind.MethodArgumentNotValidException
import org.springframework.web.bind.annotation.ExceptionHandler
import org.springframework.web.bind.annotation.RestControllerAdvice
import org.springframework.web.server.ResponseStatusException

@RestControllerAdvice
class ApiExceptionHandler {
	@ExceptionHandler(ResponseStatusException::class)
	fun handleStatus(ex: ResponseStatusException): ResponseEntity<Map<String, String>> =
		ResponseEntity
			.status(ex.statusCode)
			.body(mapOf("message" to (ex.reason ?: ex.message ?: "Error")))

	@ExceptionHandler(IllegalStateException::class)
	fun handleIllegalState(ex: IllegalStateException): ResponseEntity<Map<String, String>> =
		ResponseEntity
			.status(HttpStatus.BAD_REQUEST)
			.body(mapOf("message" to (ex.message ?: "Invalid state")))

	@ExceptionHandler(MethodArgumentNotValidException::class)
	fun handleValidation(ex: MethodArgumentNotValidException): ResponseEntity<Map<String, String>> {
		val message = ex.bindingResult.fieldErrors.firstOrNull()?.defaultMessage ?: "Validation failed"
		return ResponseEntity.status(HttpStatus.BAD_REQUEST).body(mapOf("message" to message))
	}
}
