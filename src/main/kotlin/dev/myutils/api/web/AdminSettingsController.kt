package dev.myutils.api.web

import dev.myutils.api.properties.AppProperties
import dev.myutils.api.properties.RuntimePropertiesService
import dev.myutils.api.web.dto.PropertyResponse
import dev.myutils.api.web.dto.UpdatePropertyRequest
import dev.myutils.api.web.dto.toResponse
import jakarta.validation.Valid
import org.springframework.http.HttpStatus
import org.springframework.security.core.annotation.AuthenticationPrincipal
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import org.springframework.web.server.ResponseStatusException

@RestController
@RequestMapping("/api/admin/settings")
class AdminSettingsController(
	private val runtimeProperties: RuntimePropertiesService,
) {
	@GetMapping
	fun list(): List<PropertyResponse> = runtimeProperties.listAll().map { it.toResponse() }

	@GetMapping("/{key:.+}")
	fun get(
		@PathVariable key: String,
	): PropertyResponse {
		AppProperties.find(key)
			?: throw ResponseStatusException(HttpStatus.NOT_FOUND, "Unknown property: $key")
		return runtimeProperties.listAll().first { it.key == key }.toResponse()
	}

	@PutMapping("/{key:.+}")
	fun update(
		@PathVariable key: String,
		@Valid @RequestBody body: UpdatePropertyRequest,
		@AuthenticationPrincipal email: String?,
	): PropertyResponse = runtimeProperties.update(key, body.value, updatedBy = email).toResponse()
}
